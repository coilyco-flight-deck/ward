package main

// fleet.go parses ward's dialect-2 fleet config (cli-guard pkg/fleetconfig) off
// the launch-selected config source (configsource.go, ward#653; drift-tested).

import (
	"fmt"
	"io/fs"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// loadFleetConfig parses the fleet config off the launch-selected source. A
// bundle failure errors loud at the call site, never a silent baked fallback.
func loadFleetConfig() (fleetconfig.Fleet, error) {
	src, err := selectConfigSource()
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	return loadFleetConfigFrom(src)
}

// loadBakedFleetConfig bypasses WARD_CONFIG_REF for the one init-time consumer
// (the --driver choice list, agent.go): a bad ref must not panic the binary there.
func loadBakedFleetConfig() (fleetconfig.Fleet, error) {
	return loadFleetConfigFrom(bakedConfigSource())
}

func loadFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	b, err := fs.ReadFile(src.fsys, src.fleetKDL)
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("read fleet config %s: %w", src.fleetKDL, err)
	}
	return fleetconfig.Parse(b)
}
