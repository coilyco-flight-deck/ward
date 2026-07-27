package main

// configsource.go is the baked native-policy fs.FS seam.

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// wardConfigRefEnv is retained as an ignored compatibility environment name.
const wardConfigRefEnv = "WARD_CONFIG_REF"

//go:embed fleetassets/fleet.generated.kdl
var bakedSupportAssets embed.FS

//go:embed defaultsassets/defaults.generated.kdl
var bakedDefaultsAssets embed.FS

//go:embed roleassets/role-definitions.generated.kdl
var bakedRoleAssets embed.FS

//go:embed topologyassets/topology.generated.kdl
var bakedTopologyAssets embed.FS

var bakedAssets = unionFS{primary: bakedSupportAssets, fallback: unionFS{primary: bakedDefaultsAssets, fallback: unionFS{primary: bakedRoleAssets, fallback: bakedTopologyAssets}}}

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

// Baked-layout paths are named once so the native policy loaders agree.
const (
	fleetGeneratedKDLPath           = "fleetassets/fleet.generated.kdl"
	defaultsGeneratedKDLPath        = "defaultsassets/defaults.generated.kdl"
	roleDefinitionsGeneratedKDLPath = "roleassets/role-definitions.generated.kdl"
	topologyGeneratedKDLPath        = "topologyassets/topology.generated.kdl"
)

// configSource is one policy bundle filesystem plus its native-loader paths.
type configSource struct {
	fsys fs.FS

	// fleetKDL feeds the legacy embedded fleetconfig parse path.
	fleetKDL string

	// roleDefinitionsKDL feeds the role catalog parser used by the startup-role
	// roster and launch default-harness resolution.
	roleDefinitionsKDL string

	// defaultsKDL feeds the edge smart-defaults parser.
	defaultsKDL string

	// topologyKDL feeds the edge container-topology resolver.
	topologyKDL string
}

// bakedConfigSource is the native control plane's baked AOS policy.
func bakedConfigSource() configSource {
	return configSource{
		fsys:               bakedAssets,
		fleetKDL:           fleetGeneratedKDLPath,
		roleDefinitionsKDL: roleDefinitionsGeneratedKDLPath,
		defaultsKDL:        defaultsGeneratedKDLPath,
		topologyKDL:        topologyGeneratedKDLPath,
	}
}

// bundleConfigSource supports direct parser tests for a local policy bundle.
func bundleConfigSource(dir string) configSource {
	return configSource{
		fsys:               os.DirFS(dir),
		roleDefinitionsKDL: filepath.Join("policy", "roles.kdl"),
	}
}

// sourceDesc keeps native-policy diagnostics precise.
func (s configSource) sourceDesc() string {
	return "baked native policy"
}
