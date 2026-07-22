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

// wardConfigRefEnv selects the config source ahead of operator-local config.
const wardConfigRefEnv = "WARD_CONFIG_REF"

const (
	configRefOriginGlobalConfig = "~/.ward/config.yaml config-ref"
	configRefOriginTarget       = "target metadata config-ref"
)

type configRefSelection struct {
	ref    string
	origin string
}

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

//go:embed roleassets/role-definitions.generated.kdl
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

// Baked-layout paths are named once so the runtime mount and drift tests agree.
const (
	opsForgejoGuardfilePath         = "opsassets/forgejo.guardfile.generated.kdl"
	opsForgejoSpecLockPath          = "opsassets/forgejo.swagger.lock.generated.json"
	execAssetsDir                   = "execassets"
	fleetGeneratedKDLPath           = "fleetassets/fleet.generated.kdl"
	defaultsGeneratedKDLPath        = "defaultsassets/defaults.generated.kdl"
	roleDefinitionsGeneratedKDLPath = "roleassets/role-definitions.generated.kdl"
	topologyGeneratedKDLPath        = "topologyassets/topology.generated.kdl"
)

// Bundle-layout paths: the flat .ward bundle a ref points at.
// See docs/config-source.md.
const (
	bundleExecGuardfileGlob = "guardfile.*.kdl"
)

// configSource is the launch-selected home of the KDL config bundle: one fs.FS
// plus the per-layout paths the edge build sites read.
type configSource struct {
	fsys fs.FS

	// desc names the selected source for error messages: raw config ref for
	// external bundles, empty for the baked default.
	desc string

	// auditVersion stamps the resolved bundle identity into the audit row.
	auditVersion string

	// forgejoGuardfile + forgejoSpecLock feed buildForgejoOps (specverb).
	forgejoGuardfile string
	forgejoSpecLock  string

	// fleetKDL feeds the legacy embedded fleetconfig parse path.
	fleetKDL string

	// roleDefinitionsKDL feeds the role catalog parser used by the startup-role
	// roster and launch default-harness resolution.
	roleDefinitionsKDL string

	// defaultsKDL feeds the edge smart-defaults parser.
	defaultsKDL string

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
		fsys:               bakedAssets,
		forgejoGuardfile:   opsForgejoGuardfilePath,
		forgejoSpecLock:    opsForgejoSpecLockPath,
		fleetKDL:           fleetGeneratedKDLPath,
		roleDefinitionsKDL: roleDefinitionsGeneratedKDLPath,
		defaultsKDL:        defaultsGeneratedKDLPath,
		topologyKDL:        topologyGeneratedKDLPath,
		execDir:            execAssetsDir,
		execGuardfileGlob:  "ward-kdl.*.guardfile.kdl",
	}
}

// bundleConfigSource reads the launch-selected .ward bundle layout out of dir.
func bundleConfigSource(dir string) configSource {
	return configSource{
		fsys:               os.DirFS(dir),
		roleDefinitionsKDL: filepath.Join("ward-kdl", "ward-kdl.role-definitions.kdl"),
		execDir:            ".",
		execGuardfileGlob:  bundleExecGuardfileGlob,
		execMixedDialects:  true,
	}
}

// selectConfigSource resolves the launch config for edge surfaces.
func selectConfigSource() (configSource, error) {
	selection, err := selectedConfigRefDetail()
	if err != nil {
		return configSource{}, err
	}
	return selectConfigSourceForSelection(selection)
}

func selectConfigSourceForSelection(selection configRefSelection) (configSource, error) {
	ref := selection.ref
	if ref == "" {
		return bakedConfigSource(), nil
	}
	// A set-but-unresolvable ref fails loud, never a silent baked fallback
	// (docs/config-source.md).
	if localPath, ok, err := resolveLocalConfigRef(ref); ok {
		if err != nil {
			return configSource{}, fmt.Errorf("%s: setup-generated local config path %s must exist: %w", selection.origin, localPath, err)
		}
		src, err := localConfigSource(localPath, ref)
		if err != nil {
			return configSource{}, fmt.Errorf("%s=%q: %w", selection.origin, ref, err)
		}
		return src, nil
	}
	src, err := remoteConfigSource(ref, selection.origin)
	if err != nil {
		return configSource{}, err
	}
	return src, nil
}

func remoteConfigSource(ref, origin string) (configSource, error) {
	dir, isFile := strings.CutPrefix(ref, "file://")
	if !isFile {
		// The git-ref grammar (ward#654): parse, then sync through the shared
		// TTL-cached resolver into the config-bundle cache.
		cr, err := parseConfigRef(ref)
		if err != nil {
			return configSource{}, fmt.Errorf("%s=%q: %w", origin, ref, err)
		}
		dir, err = leanRunner().resolveConfigBundle(context.Background(), cr, ref)
		if err != nil {
			return configSource{}, fmt.Errorf("%s=%q: %w", origin, ref, err)
		}
	}
	st, err := os.Stat(dir)
	if err != nil {
		return configSource{}, fmt.Errorf("%s: bundle dir: %w", origin, err)
	}
	if !st.IsDir() {
		return configSource{}, fmt.Errorf("%s: bundle path %s is not a directory", origin, dir)
	}
	src := bundleConfigSource(dir)
	src.desc = ref
	rev, err := bundleRevision(dir)
	if err != nil {
		if isFile {
			return src, nil
		}
		return configSource{}, fmt.Errorf("%s=%q: %w", origin, ref, err)
	}
	src.auditVersion = rev
	return src, nil
}

func resolveLocalConfigRef(rawRef string) (string, bool, error) {
	rawRef = strings.TrimSpace(rawRef)
	if rawRef == "" {
		return "", false, nil
	}
	if dir, ok := strings.CutPrefix(rawRef, "file://"); ok {
		return resolvePathFromInvokeCWD(dir), true, nil
	}
	if !looksLikeLocalConfigRef(rawRef) {
		return "", false, nil
	}
	localPath := resolvePathFromInvokeCWD(rawRef)
	st, err := os.Stat(localPath)
	if err != nil {
		if shouldTreatAsLocalConfigRef(rawRef) {
			return localPath, true, err
		}
		return "", false, nil
	}
	if st.IsDir() || st.Mode().IsRegular() {
		return localPath, true, nil
	}
	return localPath, true, fmt.Errorf("bundle path %s is not a file or directory", localPath)
}

func shouldTreatAsLocalConfigRef(rawRef string) bool {
	if filepath.IsAbs(rawRef) || strings.HasPrefix(rawRef, "./") || strings.HasPrefix(rawRef, "../") {
		return true
	}
	return strings.EqualFold(filepath.Ext(rawRef), ".kdl")
}

func looksLikeLocalConfigRef(rawRef string) bool {
	if shouldTreatAsLocalConfigRef(rawRef) {
		return true
	}
	_, err := os.Stat(resolvePathFromInvokeCWD(rawRef))
	return err == nil
}

func resolvePathFromInvokeCWD(p string) string {
	p = filepath.Clean(filepath.FromSlash(strings.TrimSpace(p)))
	if p == "." || p == "" {
		return resolveInvokeCWD()
	}
	if filepath.IsAbs(p) {
		return p
	}
	cwd := resolveInvokeCWD()
	if cwd == "" {
		cwd = "."
	}
	return filepath.Clean(filepath.Join(cwd, p))
}

func localConfigSource(localPath, rawRef string) (configSource, error) {
	st, err := os.Stat(localPath)
	if err != nil {
		return configSource{}, err
	}
	var src configSource
	if st.IsDir() {
		src = bundleConfigSource(localPath)
	} else {
		src = bundleFileConfigSource(localPath)
	}
	src.desc = rawRef
	revisionRoot := localPath
	if !st.IsDir() {
		revisionRoot = filepath.Dir(localPath)
	}
	rev, err := bundleRevision(revisionRoot)
	if err == nil {
		src.auditVersion = rev
	}
	return src, nil
}

func bundleFileConfigSource(file string) configSource {
	return configSource{
		fsys:              os.DirFS(filepath.Dir(file)),
		execDir:           filepath.Base(file),
		execGuardfileGlob: bundleExecGuardfileGlob,
		execMixedDialects: true,
	}
}

// sourceDesc renders the selected source for error text: the raw ref plus the
// resolved bundle sha when one exists, else the baked default.
func (s configSource) sourceDesc() string {
	if strings.TrimSpace(s.desc) == "" {
		return "baked neutral defaults"
	}
	if v := strings.TrimSpace(s.auditVersion); v != "" {
		return s.desc + " (bundle " + v + ")"
	}
	return s.desc
}

func selectedConfigRef() (string, error) {
	selection, err := selectedConfigRefDetail()
	return selection.ref, err
}

func selectedConfigRefDetail() (configRefSelection, error) {
	ref := strings.TrimSpace(os.Getenv(wardConfigRefEnv))
	if ref != "" {
		return configRefSelection{ref: ref, origin: wardConfigRefEnv}, nil
	}
	cfg, err := loadWardGlobalConfig()
	if err != nil {
		return configRefSelection{}, fmt.Errorf("read %s: %w", configRefOriginGlobalConfig, err)
	}
	ref = strings.TrimSpace(cfg.ConfigRef)
	if ref != "" {
		return configRefSelection{ref: ref, origin: configRefOriginGlobalConfig}, nil
	}
	src := bakedConfigSource()
	target, ok := coilycoTargetRepo()
	if !ok {
		return configRefSelection{}, nil
	}
	reconstructed, err := coilycoConfigRefFromTargetRepo(target, resolveInvokeCWD())
	if err != nil {
		return configRefSelection{}, fmt.Errorf("config source: active config source is %s; expected %s or %s to point at the coilyco bundle for target %s (and could not reconstruct it from target metadata: %w)", configSourceSummaryForSelection(configRefSelection{}, src), wardConfigRefEnv, configRefOriginGlobalConfig, target.slug(), err)
	}
	return configRefSelection{ref: reconstructed, origin: configRefOriginTarget}, nil
}

func configSourceSummaryForSelection(selection configRefSelection, src configSource) string {
	if strings.TrimSpace(selection.ref) == "" {
		return "baked neutral default (no external config source active)"
	}
	origin := strings.TrimSpace(selection.origin)
	if origin == "" {
		origin = wardConfigRefEnv
	}
	if strings.TrimSpace(src.auditVersion) != "" {
		return origin + "=" + selection.ref + " (bundle " + src.auditVersion + ")"
	}
	return origin + "=" + selection.ref
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
