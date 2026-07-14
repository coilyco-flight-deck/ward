package main

import (
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// TestCapabilityGuardfilesExist pins the name constants to real files so a guardfile
// rename that outran a constant can't silently drop a role's capability (ward#578).
func TestCapabilityGuardfilesExist(t *testing.T) {
	path := execAssetsDir + "/" + guardfileAWS
	if _, err := bakedAssets.ReadFile(path); err != nil {
		t.Errorf("capability guardfile %q does not exist at %s (%v); a rename must update the constant", guardfileAWS, path, err)
	}
	fleet, err := bakedAssets.ReadFile(fleetGeneratedKDLPath)
	if err != nil {
		t.Fatalf("read baked fleet config: %v", err)
	}
	if !strings.Contains(string(fleet), guardfileTailscale) {
		t.Errorf("baked fleet config does not reference %q", guardfileTailscale)
	}
}

// TestCapabilityForRole covers ward#578 + ward#547: advisor and director both hold
// the live-observe set; engineer/qa/session/unknown fall through to least-access.
func TestCapabilityForRole(t *testing.T) {
	for _, role := range []string{roleAdvisor, roleDirector} {
		if caps := capabilityForRole(role); !caps.aws || !caps.tailnet {
			t.Errorf("%s capability = %+v, want aws+tailnet from its guardfile set", role, caps)
		}
	}
	for _, role := range []string{roleEngineer, roleQA, roleSession, "nonexistent"} {
		if caps := capabilityForRole(role); caps.aws || caps.tailnet {
			t.Errorf("%s capability = %+v, want least-access (none)", role, caps)
		}
	}
}

// TestGuardfileInSet covers both membership forms: exact match in a flat list and
// a name-prefix match, with non-members rejected either way.
func TestGuardfileInSet(t *testing.T) {
	list := fleetconfig.Guardfiles{List: []string{guardfileAWS, guardfileTailscale}}
	if !guardfileInSet(guardfileAWS, list) || !guardfileInSet(guardfileTailscale, list) {
		t.Error("flat list should contain both members")
	}
	if guardfileInSet("ward-kdl.git.guardfile.kdl", list) {
		t.Error("flat list must not contain a non-member")
	}
	prefix := fleetconfig.Guardfiles{Prefix: "ward-kdl.aws"}
	if !guardfileInSet(guardfileAWS, prefix) {
		t.Error("prefix set should match the aws guardfile")
	}
	if guardfileInSet(guardfileTailscale, prefix) {
		t.Error("prefix set must not match a name outside the prefix")
	}
}

// TestResolveCapability covers the role default and the advisor's --no-tailnet
// full-isolation opt-out (ward#578).
func TestResolveCapability(t *testing.T) {
	if caps := resolveCapability(roleAdvisor); !caps.aws || !caps.tailnet {
		t.Fatalf("advisor role capability = %+v, want aws+tailnet from its guardfile set", caps)
	}
	if caps := resolveCapability(roleEngineer); caps.aws || caps.tailnet {
		t.Fatalf("engineer role capability = %+v, want least-access (none)", caps)
	}
	if caps := resolveCapabilityWithOptOut(roleAdvisor, true); caps.aws || caps.tailnet {
		t.Fatalf("advisor opt-out capability = %+v, want least-access (none)", caps)
	}
}
