package main

// fleet.go embeds + parses ward's dialect-2 fleet config (cli-guard pkg/fleetconfig):
// the agent roster + launch shape, mirrored by `make sync-fleet-assets` (drift-tested).

import (
	_ "embed"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

//go:embed fleetassets/fleet.generated.kdl
var fleetGeneratedKDL []byte

// loadFleetConfig parses the embedded fleet config under the Embedded source,
// failing closed so a broken embed is a build-time test failure, never a run.
func loadFleetConfig() (fleetconfig.Fleet, error) {
	return fleetconfig.Parse(fleetGeneratedKDL)
}
