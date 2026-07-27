package main

import (
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// TestCapabilityGuardfilesStayInFleetPolicy pins the names used to derive
// native role capabilities. The actual operator guardfiles ship with AOSguard.
func TestPolicyBoundaryCapabilityGuardfilesStayInFleetPolicy(t *testing.T) {
	fleet, err := bakedAssets.ReadFile(fleetKDLPath)
	if err != nil {
		t.Fatalf("read baked fleet config: %v", err)
	}
	if !strings.Contains(string(fleet), guardfileTailscale) {
		t.Errorf("baked fleet config does not reference %q", guardfileTailscale)
	}
}

// TestCapabilityForRole covers ward#578 + ward#547: director holds the
// live-observe set; engineer/qa/session/unknown fall through to least-access.
func TestPolicyBoundaryCapabilityForRole(t *testing.T) {
	if caps := capabilityForRole(roleDirector); !caps.aws || !caps.tailnet {
		t.Errorf("%s capability = %+v, want aws+tailnet from its guardfile set", roleDirector, caps)
	}
	for _, role := range []string{roleEngineer, roleQA, roleSession, "nonexistent"} {
		if caps := capabilityForRole(role); caps.aws || caps.tailnet {
			t.Errorf("%s capability = %+v, want least-access (none)", role, caps)
		}
	}
}

// TestGuardfileInSet covers both membership forms: exact match in a flat list and
// a name-prefix match, with non-members rejected either way.
func TestPolicyBoundaryGuardfileInSet(t *testing.T) {
	list := fleetconfig.Guardfiles{List: []string{guardfileAWS, guardfileTailscale}}
	if !guardfileInSet(guardfileAWS, list) || !guardfileInSet(guardfileTailscale, list) {
		t.Error("flat list should contain both members")
	}
	if guardfileInSet("git.kdl", list) {
		t.Error("flat list must not contain a non-member")
	}
	prefix := fleetconfig.Guardfiles{Prefix: "aws"}
	if !guardfileInSet(guardfileAWS, prefix) {
		t.Error("prefix set should match the aws guardfile")
	}
	if guardfileInSet(guardfileTailscale, prefix) {
		t.Error("prefix set must not match a name outside the prefix")
	}
}

// TestResolveCapability covers the role default and full-isolation opt-out
// path (ward#578).
func TestPolicyBoundaryResolveCapability(t *testing.T) {
	if caps := resolveCapability(roleDirector); !caps.aws || !caps.tailnet {
		t.Fatalf("director role capability = %+v, want aws+tailnet from its guardfile set", caps)
	}
	if caps := resolveCapability(roleEngineer); caps.aws || caps.tailnet {
		t.Fatalf("engineer role capability = %+v, want least-access (none)", caps)
	}
	if caps := resolveCapabilityWithOptOut(roleDirector, true); caps.aws || caps.tailnet {
		t.Fatalf("director opt-out capability = %+v, want least-access (none)", caps)
	}
}
