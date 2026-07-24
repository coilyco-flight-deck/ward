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

// currentProfileSourceProvider is deliberately native-only.
// Aguard config cannot perturb Ward's agent lifecycle.
func currentProfileSourceProvider() (configSource, ProfileProvider) {
	src := bakedConfigSource()
	return src, newProfileProvider(src)
}

// currentFleetConfigWithError resolves the baked AOS-authored fleet policy.
func currentFleetConfigWithError() (fleetconfig.Fleet, error) {
	src, provider := currentProfileSourceProvider()
	fleet, err := provider.Fleet()
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("fleet config [config source: %s]: %w", src.sourceDesc(), err)
	}
	return fleet, nil
}

// currentAgentRoleCatalogWithError resolves the baked AOS-authored role catalog.
func currentAgentRoleCatalogWithError() (agentRoleCatalog, error) {
	src, provider := currentProfileSourceProvider()
	cat, err := provider.AgentRoles()
	if err != nil {
		return agentRoleCatalog{}, fmt.Errorf("agent role catalog [config source: %s]: %w", src.sourceDesc(), err)
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
