package main

import (
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// agent_capability.go resolves a startup role's host/cloud reach from the
// embedded fleet config's per-role guardfile sets (ward#578). See docs/agent-flags.md.

// Well-known capability guardfile names a role's set can hold; ward binds each to
// the host mechanism it ships. A name outside this set grants no container capability.
const (
	guardfileAWS       = "ward-kdl.aws.guardfile.kdl"       // -> export host creds + inject AWS_* env (mount ~/.aws:ro is the fallback)
	guardfileTailscale = "ward-kdl.tailscale.guardfile.kdl" // -> join the tailnet
)

// roleCapability is the resolved host/cloud reach a role holds: the two opt-in
// mechanisms ward composes at launch. Zero value is least-access.
type roleCapability struct {
	aws     bool // deliver AWS creds: export the host chain into AWS_* env, else mount ~/.aws:ro
	tailnet bool // join the tailnet (the tailscale guardfile's declared network)
}

// capabilityForRole resolves the given role's guardfile set in the embedded fleet
// config to the host mechanisms ward composes; a broken embed fails closed to none.
func capabilityForRole(role string) roleCapability {
	fleet, err := loadFleetConfig()
	if err != nil {
		return roleCapability{}
	}
	for _, r := range fleet.Roles {
		if r.Name == role {
			return capabilityFromGuardfiles(r.Guardfiles)
		}
	}
	return roleCapability{}
}

// capabilityFromGuardfiles maps a resolved guardfile set (flat list or prefix) to
// the host mechanisms ward knows how to compose.
func capabilityFromGuardfiles(g fleetconfig.Guardfiles) roleCapability {
	return roleCapability{
		aws:     guardfileInSet(guardfileAWS, g),
		tailnet: guardfileInSet(guardfileTailscale, g),
	}
}

// guardfileInSet reports membership: a prefix set matches by name prefix, a flat
// list by exact membership.
func guardfileInSet(name string, g fleetconfig.Guardfiles) bool {
	if g.Prefix != "" {
		return strings.HasPrefix(name, g.Prefix)
	}
	for _, n := range g.List {
		if n == name {
			return true
		}
	}
	return false
}

// resolveCapability resolves the role's config-driven capability. The source of
// truth is the embedded fleet config.
func resolveCapability(role string) roleCapability {
	caps := capabilityForRole(role)
	if caps.tailnet {
		caps.aws = true
	}
	return caps
}

// resolveCapabilityWithOptOut resolves the role capability and applies the
// explicit isolation opt-out.
func resolveCapabilityWithOptOut(role string, noTailnet bool) roleCapability {
	caps := resolveCapability(role)
	if noTailnet {
		// "Stay isolated" wins over the role default + a stray tailnet grant:
		// drop the tailnet and the role-granted ~/.aws.
		caps.tailnet = false
		caps.aws = false
	}
	return caps
}
