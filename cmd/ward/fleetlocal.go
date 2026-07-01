package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// operatorLocalConfigFile is the basename of the host-local operator config
// under ~/.ward (hand-edited, git-ignored, never embedded). See docs/fleet-local.md.
const operatorLocalConfigFile = "fleet.local.kdl"

// operatorLocalConfigPath resolves ~/.ward/fleet.local.kdl. It never touches
// disk; the file may or may not exist.
func operatorLocalConfigPath() (string, error) {
	dir, err := config.GlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, operatorLocalConfigFile), nil
}

// loadOperatorLocalConfig reads ~/.ward/fleet.local.kdl through the shared
// fleetconfig parser under the OperatorLocal source. See docs/fleet-local.md.
func loadOperatorLocalConfig() (fleetconfig.Fleet, error) {
	path, err := operatorLocalConfigPath()
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	b, err := os.ReadFile(path) // #nosec G304 -- path is ward-derived under ~/.ward
	if err != nil {
		// A missing file is the empty layer, not a failure.
		if errors.Is(err, fs.ErrNotExist) {
			return fleetconfig.Fleet{}, nil
		}
		return fleetconfig.Fleet{}, fmt.Errorf("read %s: %w", path, err)
	}
	// A present-but-malformed or out-of-subset file fails closed.
	f, err := fleetconfig.ParseSource(b, fleetconfig.OperatorLocal)
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}
