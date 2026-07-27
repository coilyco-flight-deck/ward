// Package wardassets embeds Ward's canonical native policy.
package wardassets

import "embed"

// Native-policy paths name the files within NativePolicy.
const (
	NativePolicyFleetPath    = ".ward/fleet.kdl"
	NativePolicyDefaultsPath = ".ward/defaults.kdl"
	NativePolicyRolesPath    = ".ward/roles.kdl"
	NativePolicyTopologyPath = ".ward/topology.kdl"
)

//go:embed all:.ward/*.kdl
var NativePolicy embed.FS
