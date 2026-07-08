package main

import (
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/urfave/cli/v3"
)

// agent_capability.go resolves a startup role's host/cloud capability from the
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

// resolveCapability combines the role's config-driven capability with the deprecated
// --aws/--tailnet overrides (ward#578). See docs/agent-capability.md.
func resolveCapability(c *cli.Command, role string) roleCapability {
	caps := capabilityForRole(role)
	if c.Bool("aws") {
		caps.aws = true
	}
	if tailnetFlagForcesOn(c) {
		caps.tailnet = true
	}
	if c.Bool("no-tailnet") {
		// Advisor's "stay isolated" opt-out wins over the role default + a stray
		// --tailnet: drop the tailnet and the role-granted ~/.aws (explicit --aws stands).
		caps.tailnet = false
		if !c.Bool("aws") {
			caps.aws = false
		}
	}
	if caps.tailnet {
		caps.aws = true
	}
	return caps
}

// tailnetFlagForcesOn reports whether the deprecated flags explicitly ask for the
// tailnet: --tailnet, or an explicit non-auto --tailnet-mode. --no-tailnet vetoes later.
func tailnetFlagForcesOn(c *cli.Command) bool {
	if c.Bool("tailnet") {
		return true
	}
	m := strings.TrimSpace(c.String("tailnet-mode"))
	return c.IsSet("tailnet-mode") && m != "" && m != tailnetModeAuto
}
