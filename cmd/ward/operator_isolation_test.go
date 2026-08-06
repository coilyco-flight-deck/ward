package main

import "testing"

// A stale edge reference is irrelevant to every native Ward policy loader.
// This protects `ward agent` after AOSguard moved out of the Ward binary.
func TestPolicyBoundaryNativePolicyIgnoresStaleOperatorConfigRef(t *testing.T) {
	t.Setenv("WARD_CONFIG_REF", "forgejo.example.invalid/edge/aosguard@missing//.specgen")

	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("native smart defaults used stale edge ref: %v", err)
	}
	if defs.agentReservationTTL != bakedSmartDefaults().agentReservationTTL {
		t.Fatal("stale config reference changed typed defaults")
	}
	if _, err := loadLaunchConfig(); err != nil {
		t.Fatalf("typed harness adapters used stale edge ref: %v", err)
	}
	if got := currentContainerTopology(); got != containerTopologyDefaults {
		t.Fatalf("stale config reference changed typed topology: %#v", got)
	}
}

func TestPolicyBoundaryRootOmitsGeneratedOperatorSurfaces(t *testing.T) {
	root := rootCommand()
	for _, gone := range []string{"ops", "docker", "pkg"} {
		if commandNamed(root.Commands, gone) != nil {
			t.Errorf("root still exposes generated operator surface %q", gone)
		}
	}
	if commandNamed(root.Commands, "agent") == nil || commandNamed(root.Commands, "exec") == nil {
		t.Fatal("native agent or exec surface disappeared with operator cutover")
	}
}
