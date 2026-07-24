package main

import (
	"fmt"
	"sync"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// ProfileProvider is the typed seam for Ward's runtime profile data.
// The default provider preserves the baked embedded behavior.
type ProfileProvider interface {
	SmartDefaults() (smartDefaults, error)
	Fleet() (fleetconfig.Fleet, error)
	Topology() (containerTopology, error)
	AgentRoles() (agentRoleCatalog, error)
}

type configProfileProvider struct {
	src configSource

	smartDefaultsOnce sync.Once
	smartDefaults     smartDefaults
	smartDefaultsErr  error

	fleetOnce sync.Once
	fleet     fleetconfig.Fleet
	fleetErr  error

	topologyOnce sync.Once
	topology     containerTopology
	topologyErr  error

	agentRolesOnce sync.Once
	agentRoles     agentRoleCatalog
	agentRolesErr  error
}

func newProfileProvider(src configSource) ProfileProvider {
	return &configProfileProvider{src: src}
}

func bakedProfileProvider() ProfileProvider {
	return newProfileProvider(bakedConfigSource())
}

// currentFleetConfigWithError resolves the native agent fleet profile from the
// baked product data. Runtime bundles are reserved for generated edge surfaces.
func currentFleetConfigWithError() (fleetconfig.Fleet, error) {
	fleet, err := bakedProfileProvider().Fleet()
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("baked fleet config: %w", err)
	}
	return fleet, nil
}

// currentAgentRoleCatalogWithError resolves the native role catalog from baked
// product data, independent of WARD_CONFIG_REF.
func currentAgentRoleCatalogWithError() (agentRoleCatalog, error) {
	cat, err := bakedProfileProvider().AgentRoles()
	if err != nil {
		return agentRoleCatalog{}, fmt.Errorf("baked agent role catalog: %w", err)
	}
	return cat, nil
}

func (p *configProfileProvider) SmartDefaults() (smartDefaults, error) {
	p.smartDefaultsOnce.Do(func() {
		p.smartDefaults, p.smartDefaultsErr = loadSmartDefaultsFrom(p.src)
	})
	return p.smartDefaults, p.smartDefaultsErr
}

func (p *configProfileProvider) Fleet() (fleetconfig.Fleet, error) {
	p.fleetOnce.Do(func() {
		p.fleet, p.fleetErr = loadFleetConfigFrom(p.src)
	})
	return p.fleet, p.fleetErr
}

func (p *configProfileProvider) Topology() (containerTopology, error) {
	p.topologyOnce.Do(func() {
		p.topology, p.topologyErr = loadContainerTopologyFrom(p.src)
	})
	return p.topology, p.topologyErr
}

func (p *configProfileProvider) AgentRoles() (agentRoleCatalog, error) {
	p.agentRolesOnce.Do(func() {
		p.agentRoles, p.agentRolesErr = loadAgentRoleCatalogFrom(p.src)
	})
	return p.agentRoles, p.agentRolesErr
}
