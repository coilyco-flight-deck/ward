package main

// configsource.go is the edge-surface fs.FS-at-launch config-resolve seam.
// Baked embeds default; WARD_CONFIG_REF only steers edge-mounted surfaces.

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// wardConfigRefEnv selects the config source. Unset means the baked default.
const wardConfigRefEnv = "WARD_CONFIG_REF"

// bakedAssets is the baked neutral default: mirrors of .ward/ward-kdl plus the
// admin guardfile and smart defaults.

//go:embed opsassets/*.generated.kdl opsassets/*.generated.json execassets/*.guardfile.kdl
var bakedMainAssets embed.FS

//go:embed opsassets/forgejo-admin.guardfile.kdl fleetassets/fleet.generated.kdl
var bakedWardAssets embed.FS

//go:embed defaultsassets/defaults.generated.kdl
var bakedDefaultsAssets embed.FS

//go:embed topologyassets/topology.generated.kdl
var bakedTopologyAssets embed.FS

var bakedAssets = unionFS{primary: unionFS{primary: bakedMainAssets, fallback: bakedWardAssets}, fallback: unionFS{primary: bakedDefaultsAssets, fallback: bakedTopologyAssets}}

type unionFS struct {
	primary  fs.FS
	fallback fs.FS
}

func (u unionFS) Open(name string) (fs.File, error) {
	f, err := u.primary.Open(name)
	if err == nil || !os.IsNotExist(err) {
		return f, err
	}
	return u.fallback.Open(name)
}

func (u unionFS) ReadFile(name string) ([]byte, error) {
	b, err := fs.ReadFile(u.primary, name)
	if err == nil || !os.IsNotExist(err) {
		return b, err
	}
	return fs.ReadFile(u.fallback, name)
}

// Baked-layout paths, named once so the runtime mount and the drift tests
// agree; the admin path is the exec-dialect remote-exec slice (ward#81).
const (
	opsForgejoGuardfilePath      = "opsassets/forgejo.guardfile.generated.kdl"
	opsForgejoSpecLockPath       = "opsassets/forgejo.swagger.lock.generated.json"
	opsForgejoAdminGuardfilePath = "opsassets/forgejo-admin.guardfile.kdl"
	execAssetsDir                = "execassets"
	fleetGeneratedKDLPath        = "fleetassets/fleet.generated.kdl"
	defaultsGeneratedKDLPath     = "defaultsassets/defaults.generated.kdl"
	topologyGeneratedKDLPath     = "topologyassets/topology.generated.kdl"
)

// Bundle-layout paths: the flat .ward bundle a ref points at (aos#332's landed
// layout, identical to this repo's .ward/ward-kdl/). See docs/config-source.md.
const (
	bundleForgejoGuardfilePath      = "ward-kdl.forgejo.guardfile.kdl"
	bundleForgejoSpecLockPath       = "forgejo.swagger.lock.json"
	bundleForgejoAdminGuardfilePath = "forgejo-admin.guardfile.kdl"
	bundleFleetKDLPath              = "ward-kdl.fleet.kdl"
	bundleDefaultsKDLPath           = "ward-kdl.defaults.kdl"
	bundleTopologyKDLPath           = "ward-kdl.topology.kdl"
)

// configSource is the launch-selected home of the KDL config bundle: one fs.FS
// plus the per-layout paths the edge build sites read.
type configSource struct {
	fsys fs.FS

	// auditVersion stamps the resolved bundle identity into the audit row.
	auditVersion string

	// forgejoGuardfile + forgejoSpecLock feed buildForgejoOps (specverb).
	forgejoGuardfile string
	forgejoSpecLock  string

	// adminGuardfile feeds graftForgejoAdminExec; a source that omits it
	// withholds the admin surface - absent at compile time, guardfile-style.
	adminGuardfile string

	// fleetKDL feeds the edge fleetconfig parse path.
	fleetKDL string

	// defaultsKDL feeds the edge smart-defaults parser.
	defaultsKDL string

	// topologyKDL feeds the edge container-topology resolver.
	topologyKDL string

	// execDir is scanned by mountWardKdlExec; execMixedDialects marks a bundle
	// dir where spec-dialect files sit beside exec ones and are filtered out.
	execDir           string
	execMixedDialects bool
}

// bakedConfigSource is the neutral default: the embedded assets, byte-for-byte
// today's behavior. The pre-filtered execassets mirror scans unfiltered.
func bakedConfigSource() configSource {
	return configSource{
		fsys:             bakedAssets,
		forgejoGuardfile: opsForgejoGuardfilePath,
		forgejoSpecLock:  opsForgejoSpecLockPath,
		adminGuardfile:   opsForgejoAdminGuardfilePath,
		fleetKDL:         fleetGeneratedKDLPath,
		defaultsKDL:      defaultsGeneratedKDLPath,
		topologyKDL:      topologyGeneratedKDLPath,
		execDir:          execAssetsDir,
	}
}

// bundleConfigSource reads the flat .ward bundle layout out of dir.
func bundleConfigSource(dir string) configSource {
	return configSource{
		fsys:              os.DirFS(dir),
		forgejoGuardfile:  bundleForgejoGuardfilePath,
		forgejoSpecLock:   bundleForgejoSpecLockPath,
		adminGuardfile:    bundleForgejoAdminGuardfilePath,
		fleetKDL:          bundleFleetKDLPath,
		defaultsKDL:       bundleDefaultsKDLPath,
		topologyKDL:       bundleTopologyKDLPath,
		execDir:           ".",
		execMixedDialects: true,
	}
}

// selectConfigSource resolves WARD_CONFIG_REF for edge surfaces, rereading
// the env per call (cheap, testable; it cannot change mid-process).
func selectConfigSource() (configSource, error) {
	ref := strings.TrimSpace(os.Getenv(wardConfigRefEnv))
	if ref == "" {
		src := bakedConfigSource()
		if target, ok := coilycoTargetSlug(); ok {
			return configSource{}, fmt.Errorf("%s: active config source is %s; expected WARD_CONFIG_REF to point at the coilyco bundle for target %s", wardConfigRefEnv, configSourceSummary(ref, src), target)
		}
		return src, nil
	}
	// A set-but-unresolvable ref fails loud, never a silent baked fallback
	// (docs/config-source.md).
	dir, isFile := strings.CutPrefix(ref, "file://")
	if !isFile {
		// The git-ref grammar (ward#654): parse, then sync through the shared
		// TTL-cached resolver into the config-bundle cache.
		cr, err := parseConfigRef(ref)
		if err != nil {
			return configSource{}, fmt.Errorf("%s=%q: %w", wardConfigRefEnv, ref, err)
		}
		dir, err = leanRunner().resolveConfigBundle(context.Background(), cr, ref)
		if err != nil {
			return configSource{}, fmt.Errorf("%s=%q: %w", wardConfigRefEnv, ref, err)
		}
	}
	st, err := os.Stat(dir)
	if err != nil {
		return configSource{}, fmt.Errorf("%s: bundle dir: %w", wardConfigRefEnv, err)
	}
	if !st.IsDir() {
		return configSource{}, fmt.Errorf("%s: bundle path %s is not a directory", wardConfigRefEnv, dir)
	}
	src := bundleConfigSource(dir)
	rev, err := bundleRevision(dir)
	if err != nil {
		if isFile {
			return src, nil
		}
		return configSource{}, fmt.Errorf("%s=%q: %w", wardConfigRefEnv, ref, err)
	}
	src.auditVersion = rev
	return src, nil
}

func configSourceSummary(rawRef string, src configSource) string {
	if strings.TrimSpace(rawRef) == "" {
		return "baked neutral default (no external config source active)"
	}
	if strings.TrimSpace(src.auditVersion) != "" {
		return wardConfigRefEnv + "=" + rawRef + " (bundle " + src.auditVersion + ")"
	}
	return wardConfigRefEnv + "=" + rawRef
}

func coilycoTargetSlug() (string, bool) {
	owner := strings.TrimSpace(os.Getenv("WARD_TARGET_OWNER"))
	repo := strings.TrimSpace(os.Getenv("WARD_TARGET_REPO"))
	if strings.HasPrefix(owner, "coilyco") || strings.HasPrefix(repo, "coilyco") {
		if repo != "" {
			return repo, true
		}
		return owner, true
	}
	return "", false
}

// coreRuntimeConfigSource is ward-owned runtime data: the baked neutral default
// only. Core agent/container paths do not depend on WARD_CONFIG_REF parsing.
func coreRuntimeConfigSource() configSource {
	return bakedConfigSource()
}

// bundleRevision returns the git HEAD of dir when dir is a checkout; empty
// string means the source is not a git repo or has no resolvable HEAD yet.
func bundleRevision(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").CombinedOutput() // #nosec G702 -- fixed argv, repo-root dir is controlled
	if err != nil {
		return "", fmt.Errorf("resolve bundle sha in %s: %w (%s)", dir, err, strings.TrimSpace(string(out)))
	}
	rev := strings.TrimSpace(string(out))
	if rev == "" {
		return "", fmt.Errorf("resolve bundle sha in %s: empty HEAD", dir)
	}
	return rev, nil
}

// execDialectMarker mirrors the `make sync-exec-assets` filter: exec-dialect
// guardfiles carry an indented `exec <bin>` line, spec-dialect ones do not.
var execDialectMarker = regexp.MustCompile(`(?m)^[ \t]+exec `)

// isExecDialectGuardfile reports whether a mixed bundle dir should mount
// gfBytes through the exec scan (spec-dialect siblings ride ops.go instead).
func isExecDialectGuardfile(gfBytes []byte) bool {
	return execDialectMarker.Match(gfBytes)
}
