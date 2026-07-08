package main

// configsource.go is the fs.FS-at-launch config-resolve seam (ward#653): baked
// embeds by default, a live bundle when WARD_CONFIG_REF is set: docs/config-source.md.

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// wardConfigRefEnv selects the config source (ward#650); unset means the baked
// neutral default - that is the whole precedence.
const wardConfigRefEnv = "WARD_CONFIG_REF"

// bakedAssets is the baked neutral default: drift-tested mirrors of
// .ward/ward-kdl (`make sync-*-assets`) plus the admin guardfile (ward#81).

//go:embed opsassets/*.generated.kdl opsassets/*.generated.json execassets/*.guardfile.kdl
//go:embed opsassets/forgejo-admin.guardfile.kdl fleetassets/fleet.generated.kdl
var bakedAssets embed.FS

// Baked-layout paths, named once so the runtime mount and the drift tests
// agree; the admin path is the exec-dialect remote-exec slice (ward#81).
const (
	opsForgejoGuardfilePath      = "opsassets/forgejo.guardfile.generated.kdl"
	opsForgejoSpecLockPath       = "opsassets/forgejo.swagger.lock.generated.json"
	opsForgejoAdminGuardfilePath = "opsassets/forgejo-admin.guardfile.kdl"
	execAssetsDir                = "execassets"
	fleetGeneratedKDLPath        = "fleetassets/fleet.generated.kdl"
)

// Bundle-layout paths: the flat .ward bundle a ref points at (aos#332's landed
// layout, identical to this repo's .ward/ward-kdl/). See docs/config-source.md.
const (
	bundleForgejoGuardfilePath      = "ward-kdl.forgejo.guardfile.kdl"
	bundleForgejoSpecLockPath       = "forgejo.swagger.lock.json"
	bundleForgejoAdminGuardfilePath = "forgejo-admin.guardfile.kdl"
	bundleFleetKDLPath              = "ward-kdl.fleet.kdl"
)

// configSource is the launch-selected home of the KDL config bundle: one fs.FS
// plus the per-layout paths the three build sites read.
type configSource struct {
	fsys fs.FS

	// forgejoGuardfile + forgejoSpecLock feed buildForgejoOps (specverb).
	forgejoGuardfile string
	forgejoSpecLock  string

	// adminGuardfile feeds graftForgejoAdminExec; a source that omits it
	// withholds the admin surface - absent at compile time, guardfile-style.
	adminGuardfile string

	// fleetKDL feeds loadFleetConfig (dialect-2 fleetconfig).
	fleetKDL string

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
		execDir:           ".",
		execMixedDialects: true,
	}
}

// selectConfigSource resolves WARD_CONFIG_REF into the launch config source,
// rereading the env per call (cheap, testable; it cannot change mid-process).
func selectConfigSource() (configSource, error) {
	ref := strings.TrimSpace(os.Getenv(wardConfigRefEnv))
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
	return bundleConfigSource(dir), nil
}

// execDialectMarker mirrors the `make sync-exec-assets` filter: exec-dialect
// guardfiles carry an indented `exec <bin>` line, spec-dialect ones do not.
var execDialectMarker = regexp.MustCompile(`(?m)^[ \t]+exec `)

// isExecDialectGuardfile reports whether a mixed bundle dir should mount
// gfBytes through the exec scan (spec-dialect siblings ride ops.go instead).
func isExecDialectGuardfile(gfBytes []byte) bool {
	return execDialectMarker.Match(gfBytes)
}
