package main

import (
	"fmt"
	"strings"
)

// agent_semantic_capabilities.go carries ward's semantic role postures, separate
// from the KDL edge surfaces ward-kdl owns.

type semanticCapability string

const (
	semanticCapabilityRead              semanticCapability = "read"
	semanticCapabilityProjectManagement semanticCapability = "project-management"
	semanticCapabilityEngineering       semanticCapability = "engineering"
	semanticCapabilityOps               semanticCapability = "ops"
	semanticCapabilityAdmin             semanticCapability = "admin"
)

var semanticCapabilityOrder = []semanticCapability{
	semanticCapabilityRead,
	semanticCapabilityProjectManagement,
	semanticCapabilityEngineering,
	semanticCapabilityOps,
	semanticCapabilityAdmin,
}

var semanticCapabilityByName = func() map[string]semanticCapability {
	out := map[string]semanticCapability{}
	for _, capability := range semanticCapabilityOrder {
		out[string(capability)] = capability
	}
	return out
}()

// semanticCapabilitySet is a composable semantic role posture. The zero value is
// empty, and the set methods always return a copy so presets stay reusable.
type semanticCapabilitySet map[semanticCapability]bool

// semanticCapabilities returns a set populated with the given capabilities.
func semanticCapabilities(caps ...semanticCapability) semanticCapabilitySet {
	out := semanticCapabilitySet{}
	return out.With(caps...)
}

// clone returns a copy so callers can compose without mutating the source preset.
func (s semanticCapabilitySet) clone() semanticCapabilitySet {
	if len(s) == 0 {
		return semanticCapabilitySet{}
	}
	out := semanticCapabilitySet{}
	for capability := range s {
		out[capability] = true
	}
	return out
}

// With returns a copy of the set with the given capabilities added.
func (s semanticCapabilitySet) With(caps ...semanticCapability) semanticCapabilitySet {
	out := s.clone()
	if out == nil {
		out = semanticCapabilitySet{}
	}
	for _, capability := range caps {
		out[capability] = true
	}
	return out
}

// Has reports whether the set holds one semantic capability.
func (s semanticCapabilitySet) Has(capability semanticCapability) bool {
	return s[capability]
}

// Names renders the set in ward's stable semantic order.
func (s semanticCapabilitySet) Names() []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for _, capability := range semanticCapabilityOrder {
		if s.Has(capability) {
			out = append(out, string(capability))
		}
	}
	return out
}

// String renders the set for docs and terse status lines.
func (s semanticCapabilitySet) String() string {
	return strings.Join(s.Names(), " + ")
}

// semanticCapabilitiesForRole resolves the ward-owned semantic preset for one
// startup role. Named roles are presets, not the only possible model.
func semanticCapabilitiesForRole(role string) semanticCapabilitySet {
	if defs, err := cachedEmbeddedAgentRoleCatalog(); err == nil {
		if def, ok := defs.Definitions[role]; ok {
			return def.Capabilities.clone()
		}
	}
	switch role {
	case roleOps:
		return semanticCapabilities(semanticCapabilityRead, semanticCapabilityOps)
	case roleAdmin:
		return semanticCapabilities(
			semanticCapabilityRead,
			semanticCapabilityProjectManagement,
			semanticCapabilityEngineering,
			semanticCapabilityOps,
			semanticCapabilityAdmin,
		)
	default:
		return semanticCapabilitySet{}
	}
}

// semanticCapabilitiesCompose adds explicit extras onto an existing semantic set.
func semanticCapabilitiesCompose(base semanticCapabilitySet, extras ...semanticCapability) semanticCapabilitySet {
	return base.With(extras...)
}

// semanticCapabilitiesFromNames composes a set from ward's semantic names.
func semanticCapabilitiesFromNames(names ...string) (semanticCapabilitySet, error) {
	out := semanticCapabilitySet{}
	for _, name := range names {
		capability, ok := semanticCapabilityByName[strings.TrimSpace(name)]
		if !ok {
			return semanticCapabilitySet{}, fmt.Errorf("unknown semantic capability %q", name)
		}
		out[capability] = true
	}
	return out, nil
}
