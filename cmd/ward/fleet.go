package main

// fleet.go parses ward's dialect-2 fleet config from ward-owned runtime data.
// Edge bundle selection stays on the configsource seam, core agents stay baked.

import (
	"bytes"
	"fmt"
	"io/fs"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// loadFleetConfig resolves ward's built-in frontier defaults over the baked
// fleet config and fails loud only on baked drift or parse failure.
func loadFleetConfig() (fleetconfig.Fleet, error) {
	return loadFleetConfigFrom(coreRuntimeConfigSource())
}

func loadFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	raw, err := loadRawFleetConfigFrom(src)
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	return resolveEffectiveFleet(raw)
}

func loadRawFleetConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	if src.fleetKDL != "" && src.agentsKDL == "" && src.rolesKDL == "" {
		b, err := fs.ReadFile(src.fsys, src.fleetKDL)
		if err != nil {
			return fleetconfig.Fleet{}, fmt.Errorf("read fleet config %s: %w", src.fleetKDL, err)
		}
		return fleetconfig.Parse(b)
	}
	if src.agentsKDL == "" {
		return fleetconfig.Fleet{}, fmt.Errorf("read fleet config: missing agents bundle path")
	}
	agents, err := loadBundleAgentsConfigFrom(src)
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	roles, err := loadBundleRolesFrom(src)
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	agents.Roles = roles
	return agents, nil
}

func loadBundleAgentsConfigFrom(src configSource) (fleetconfig.Fleet, error) {
	b, err := fs.ReadFile(src.fsys, src.agentsKDL)
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("read fleet config %s: %w", src.agentsKDL, err)
	}
	return fleetconfig.Parse(renameBundleRootNode(b, "agents", "fleet"))
}

func renameBundleRootNode(src []byte, from, to string) []byte {
	return bytes.Replace(src, []byte(from+" {"), []byte(to+" {"), 1)
}

func loadBundleRolesFrom(src configSource) ([]fleetconfig.Role, error) {
	b, err := fs.ReadFile(src.fsys, src.rolesKDL)
	if err != nil {
		return nil, fmt.Errorf("read fleet roles %s: %w", src.rolesKDL, err)
	}
	return parseBundleRoles(b)
}
