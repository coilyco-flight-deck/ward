package main

import (
	"os"
	"path/filepath"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/urfave/cli/v3"
)

// TestCapabilityGuardfilesExist pins the name constants to real files so a guardfile
// rename that outran a constant can't silently drop a role's capability (ward#578).
func TestCapabilityGuardfilesExist(t *testing.T) {
	for _, name := range []string{guardfileAWS, guardfileTailscale} {
		// ward-kdl moved to the .ward/ config dir (ward#435).
		path := filepath.Join("..", "..", ".ward", "ward-kdl", name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("capability guardfile %q does not exist at %s (%v); a rename must update the constant", name, path, err)
		}
	}
}

// TestCapabilityForRole covers ward#578 + ward#547: advisor and director both hold
// the live-observe set; engineer/session/unknown fall through to least-access.
func TestCapabilityForRole(t *testing.T) {
	for _, role := range []string{roleAdvisor, roleDirector} {
		if cap := capabilityForRole(role); !cap.aws || !cap.tailnet {
			t.Errorf("%s capability = %+v, want aws+tailnet from its guardfile set", role, cap)
		}
	}
	for _, role := range []string{roleEngineer, roleSession, "nonexistent"} {
		if cap := capabilityForRole(role); cap.aws || cap.tailnet {
			t.Errorf("%s capability = %+v, want least-access (none)", role, cap)
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

// TestResolveCapability covers the role default + deprecated-flag overrides and the
// advisor's --no-tailnet full-isolation opt-out (ward#578).
func TestResolveCapability(t *testing.T) {
	cases := []struct {
		name        string
		role        string
		flags       []cli.Flag
		argv        []string
		wantAWS     bool
		wantTailnet bool
	}{
		{
			name:        "advisor default holds live-observe set",
			role:        roleAdvisor,
			flags:       agentAdvisorFlags(),
			argv:        []string{"advisor", "o/r#1", "q"},
			wantAWS:     true,
			wantTailnet: true,
		},
		{
			name:        "advisor --no-tailnet fully isolates",
			role:        roleAdvisor,
			flags:       agentAdvisorFlags(),
			argv:        []string{"advisor", "o/r#1", "q", "--no-tailnet"},
			wantAWS:     false,
			wantTailnet: false,
		},
		{
			name:        "advisor --no-tailnet --aws keeps the explicit aws mount",
			role:        roleAdvisor,
			flags:       agentAdvisorFlags(),
			argv:        []string{"advisor", "o/r#1", "q", "--no-tailnet", "--aws"},
			wantAWS:     true,
			wantTailnet: false,
		},
		{
			name:        "engineer default holds nothing",
			role:        roleEngineer,
			flags:       agentSurfaceFlags(),
			argv:        []string{"engineer", "o/r#1"},
			wantAWS:     false,
			wantTailnet: false,
		},
		{
			name:        "engineer --aws force-mounts aws only",
			role:        roleEngineer,
			flags:       agentSurfaceFlags(),
			argv:        []string{"engineer", "o/r#1", "--aws"},
			wantAWS:     true,
			wantTailnet: false,
		},
		{
			name:        "engineer --tailnet implies aws",
			role:        roleEngineer,
			flags:       agentSurfaceFlags(),
			argv:        []string{"engineer", "o/r#1", "--tailnet"},
			wantAWS:     true,
			wantTailnet: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := parseCommandForTest(t, tc.flags, tc.argv)
			cap := resolveCapability(cmd, tc.role)
			if cap.aws != tc.wantAWS || cap.tailnet != tc.wantTailnet {
				t.Errorf("resolveCapability(%s) = %+v, want aws=%v tailnet=%v", tc.role, cap, tc.wantAWS, tc.wantTailnet)
			}
		})
	}
}
