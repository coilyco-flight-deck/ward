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

func currentProfileSourceProvider() (configSource, ProfileProvider, error) {
	src, err := selectConfigSource()
	if err != nil {
		return configSource{}, nil, err
	}
	return src, newProfileProvider(src), nil
}

func bakedProfileProvider() ProfileProvider {
	return newProfileProvider(bakedConfigSource())
}

// currentFleetConfigWithError resolves the active fleet config from the selected
// bundle when one exists, else the baked default.
func currentFleetConfigWithError() (fleetconfig.Fleet, error) {
	src, provider, err := currentProfileSourceProvider()
	if err != nil {
		return fleetconfig.Fleet{}, err
	}
	fleet, err := provider.Fleet()
	if err != nil {
		return fleetconfig.Fleet{}, fmt.Errorf("fleet config [config source: %s]: %w", src.sourceDesc(), err)
	}
	return fleet, nil
}

// currentAgentRoleCatalogWithError resolves the active role catalog from the
// selected bundle when one exists, else the baked default.
func currentAgentRoleCatalogWithError() (agentRoleCatalog, error) {
	src, provider, err := currentProfileSourceProvider()
	if err != nil {
		return agentRoleCatalog{}, err
	}
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
