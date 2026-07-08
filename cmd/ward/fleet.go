package main

// fleet.go parses ward's dialect-2 fleet config (cli-guard pkg/fleetconfig) off
// the launch-selected config source (configsource.go, ward#653; drift-tested).

import (
	"fmt"
	"io/fs"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// loadFleetConfig resolves ward's built-in frontier defaults over the selected
// fleet config, returning the effective roster and failing loud on bundle error.
func loadFleetConfig() (fleetconfig.Fleet, error) {
	src, err := selectConfigSource()
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	return loadFleetConfigFrom(src)
}

func loadFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	raw, err := loadRawFleetConfigFrom(src)
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	return resolveEffectiveFleet(raw)
}

func loadRawFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	b, err := fs.ReadFile(src.fsys, src.fleetKDL)
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("read fleet config %s: %w", src.fleetKDL, err)
	}
	return fleetconfig.Parse(b)
}
