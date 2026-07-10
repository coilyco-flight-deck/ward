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
	"path/filepath"
	"regexp"
	"strings"
)

// wardConfigRefEnv selects the config source. Unset means the baked default.
const wardConfigRefEnv = "WARD_CONFIG_REF"

// bakedAssets is the baked neutral default: mirrors of .ward/ward-kdl plus the
// smart defaults.

//go:embed opsassets/*.generated.kdl
//go:embed opsassets/*.generated.json
var bakedOpsAssets embed.FS

//go:embed execassets/*.guardfile.kdl
//go:embed fleetassets/fleet.generated.kdl
var bakedSupportAssets embed.FS

//go:embed defaultsassets/defaults.generated.kdl
var bakedDefaultsAssets embed.FS

//go:embed roleassets/roles.generated.kdl
var bakedRoleAssets embed.FS

//go:embed topologyassets/topology.generated.kdl
var bakedTopologyAssets embed.FS

var bakedAssets = unionFS{primary: unionFS{primary: bakedOpsAssets, fallback: bakedSupportAssets}, fallback: unionFS{primary: bakedDefaultsAssets, fallback: unionFS{primary: bakedRoleAssets, fallback: bakedTopologyAssets}}}

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
// agree.
const (
	opsForgejoGuardfilePath  = "opsassets/forgejo.guardfile.generated.kdl"
	opsForgejoSpecLockPath   = "opsassets/forgejo.swagger.lock.generated.json"
	execAssetsDir            = "execassets"
	fleetGeneratedKDLPath    = "fleetassets/fleet.generated.kdl"
	defaultsGeneratedKDLPath = "defaultsassets/defaults.generated.kdl"
	rolesGeneratedKDLPath    = "roleassets/roles.generated.kdl"
	topologyGeneratedKDLPath = "topologyassets/topology.generated.kdl"
)

// Bundle-layout paths: the flat .ward bundle a ref points at. See docs/config-source.md.
const (
	// The read/write/admin tier guardfiles are role-facing, not the ops CLI
	// surface. See docs/config-source.md.
	bundleForgejoGuardfilePath = "guardfile.forgejo.kdl"
	bundleForgejoSpecLockPath  = "forgejo.swagger.lock.json"
	bundleAgentsKDLPath        = "agents.kdl"
	bundleRolesKDLPath         = "roles.kdl"
	bundleDefaultsKDLPath      = "defaults.kdl"
	bundleReposKDLPath         = "repos.kdl"
	bundleTopologyKDLPath      = "ward-kdl.topology.kdl"
	bundleExecGuardfileGlob    = "guardfile.*.kdl"
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

	// fleetKDL feeds the legacy embedded fleetconfig parse path.
	fleetKDL string

	// agentsKDL + rolesKDL feed the split bundle fleetconfig parse path.
	agentsKDL string
	rolesKDL  string

	// defaultsKDL feeds the edge smart-defaults parser.
	defaultsKDL string

	// reposKDL feeds the split bundle smart-defaults repo-authority parser.
	reposKDL string

	// topologyKDL feeds the edge container-topology resolver.
	topologyKDL string

	// execDir is scanned by mountWardKdlExec; execMixedDialects marks a bundle
	// dir where spec-dialect files sit beside exec ones and are filtered out.
	execDir           string
	execGuardfileGlob string
	execMixedDialects bool
}

// bakedConfigSource is the neutral default: the embedded assets, byte-for-byte
// today's behavior. The pre-filtered execassets mirror scans unfiltered.
func bakedConfigSource() configSource {
	return configSource{
		fsys:              bakedAssets,
		forgejoGuardfile:  opsForgejoGuardfilePath,
		forgejoSpecLock:   opsForgejoSpecLockPath,
		fleetKDL:          fleetGeneratedKDLPath,
		defaultsKDL:       defaultsGeneratedKDLPath,
		topologyKDL:       topologyGeneratedKDLPath,
		execDir:           execAssetsDir,
		execGuardfileGlob: "ward-kdl.*.guardfile.kdl",
	}
}

// bundleConfigSource reads the flat .ward bundle layout out of dir.
func bundleConfigSource(dir string) configSource {
	return configSource{
		fsys:              os.DirFS(dir),
		forgejoGuardfile:  bundleForgejoGuardfilePath,
		forgejoSpecLock:   bundleForgejoSpecLockPath,
		agentsKDL:         bundleAgentsKDLPath,
		rolesKDL:          bundleRolesKDLPath,
		defaultsKDL:       bundleDefaultsKDLPath,
		reposKDL:          bundleReposKDLPath,
		topologyKDL:       bundleTopologyKDLPath,
		execDir:           ".",
		execGuardfileGlob: bundleExecGuardfileGlob,
		execMixedDialects: true,
	}
}

// selectConfigSource resolves WARD_CONFIG_REF for edge surfaces, rereading
// the env per call (cheap, testable; it cannot change mid-process).
func selectConfigSource() (configSource, error) {
	ref, err := selectedConfigRef()
	if err != nil {
		return configSource{}, err
	}
	if ref == "" {
		return bakedConfigSource(), nil
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

func selectedConfigRef() (string, error) {
	ref := strings.TrimSpace(os.Getenv(wardConfigRefEnv))
	if ref != "" {
		return ref, nil
	}
	src := bakedConfigSource()
	target, ok := coilycoTargetRepo()
	if !ok {
		return "", nil
	}
	if os.Getenv("WARD_READONLY") == "1" {
		reconstructed, err := coilycoConfigRefFromTargetRepo(target, resolveInvokeCWD())
		if err != nil {
			return "", fmt.Errorf("%s: active config source is %s; expected WARD_CONFIG_REF to point at the coilyco bundle for target %s (and could not reconstruct it from target metadata: %w)", wardConfigRefEnv, configSourceSummary(ref, src), target.slug(), err)
		}
		return reconstructed, nil
	}
	return "", fmt.Errorf("%s: active config source is %s; expected WARD_CONFIG_REF to point at the coilyco bundle for target %s", wardConfigRefEnv, configSourceSummary(ref, src), target.slug())
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

func coilycoTargetRepo() (targetRepo, bool) {
	owner := strings.TrimSpace(os.Getenv("WARD_TARGET_OWNER"))
	repo := strings.TrimSpace(os.Getenv("WARD_TARGET_REPO"))
	if strings.HasPrefix(owner, "coilyco") || strings.HasPrefix(repo, "coilyco") {
		if owner != "" && repo != "" {
			if o, n, ok := splitOwnerName(repo); ok {
				return targetRepo{Owner: o, Name: n}, true
			}
			return targetRepo{Owner: owner, Name: repo}, true
		}
	}
	return targetRepo{}, false
}

func isCoilycoRepo(repo targetRepo) bool {
	return strings.HasPrefix(repo.Owner, "coilyco") || strings.HasPrefix(repo.Name, "coilyco")
}

func coilycoConfigRefFromTargetRepo(repo targetRepo, cwd string) (string, error) {
	if !isCoilycoRepo(repo) {
		return "", nil
	}
	if cwd == "" {
		return "", fmt.Errorf("cannot resolve the current working directory")
	}
	out, err := exec.Command("git", "-C", filepath.Clean(cwd), "rev-parse", "HEAD").CombinedOutput() // #nosec G204 -- fixed argv, repo-root cwd is controlled
	if err != nil {
		return "", fmt.Errorf("resolve config ref for %s from %s: %w (%s)", repo.slug(), cwd, err, strings.TrimSpace(string(out)))
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("resolve config ref for %s from %s: empty HEAD", repo.slug(), cwd)
	}
	return fmt.Sprintf("%s/%s/%s@%s//.ward", forgejoCanonicalHost(), repo.Owner, repo.Name, sha), nil
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
