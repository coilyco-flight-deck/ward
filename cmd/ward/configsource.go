package main

// configsource.go is the baked native-policy fs.FS seam.

import (
	"io/fs"
	"os"

	wardassets "github.com/coilyco-flight-deck/ward"
)

// wardConfigRefEnv is retained as an ignored compatibility environment name.
const wardConfigRefEnv = "WARD_CONFIG_REF"

var bakedAssets = wardassets.NativePolicy

// Native-policy paths are named once so the loaders agree.
const (
	fleetKDLPath           = wardassets.NativePolicyFleetPath
	defaultsKDLPath        = wardassets.NativePolicyDefaultsPath
	roleDefinitionsKDLPath = wardassets.NativePolicyRolesPath
	topologyKDLPath        = wardassets.NativePolicyTopologyPath
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

// bakedConfigSource is the native control plane's baked Ward policy.
func bakedConfigSource() configSource {
	return configSource{
		fsys:               bakedAssets,
		fleetKDL:           fleetKDLPath,
		roleDefinitionsKDL: roleDefinitionsKDLPath,
		defaultsKDL:        defaultsKDLPath,
		topologyKDL:        topologyKDLPath,
	}
}

// bundleConfigSource supports direct parser tests for a local policy bundle.
func bundleConfigSource(dir string) configSource {
	return configSource{
		fsys:               os.DirFS(dir),
		roleDefinitionsKDL: "roles.kdl",
	}
}

// sourceDesc keeps native-policy diagnostics precise.
func (s configSource) sourceDesc() string {
	return "baked native policy"
}
