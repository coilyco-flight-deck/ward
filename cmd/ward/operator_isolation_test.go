package main

import (
	"reflect"
	"testing"
)

// A stale edge reference is irrelevant to every native Ward policy loader.
// This protects `ward agent` after AOSguard moved out of the Ward binary.
func TestPolicyBoundaryNativePolicyIgnoresStaleOperatorConfigRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "forgejo.example.invalid/edge/aosguard@missing//.specgen")

	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("native smart defaults used stale edge ref: %v", err)
	}
	if !reflect.DeepEqual(defs, canonicalSmartDefaults(t)) {
		t.Fatal("native smart defaults no longer match baked policy")
	}
	if _, err := currentFleetConfigWithError(); err != nil {
		t.Fatalf("native fleet used stale edge ref: %v", err)
	}
	if _, err := currentAgentRoleCatalogWithError(); err != nil {
		t.Fatalf("native role catalog used stale edge ref: %v", err)
	}
	if _, err := currentContainerTopologyWithError(); err != nil {
		t.Fatalf("native topology used stale edge ref: %v", err)
	}
}

func TestPolicyBoundaryRootOmitsGeneratedOperatorSurfaces(t *testing.T) {
	root := rootCommand()
	for _, gone := range []string{"ops", "docker", "kubectl", "pkg"} {
		if commandNamed(root.Commands, gone) != nil {
			t.Errorf("root still exposes generated operator surface %q", gone)
		}
	}
	if commandNamed(root.Commands, "agent") == nil || commandNamed(root.Commands, "exec") == nil {
		t.Fatal("native agent or exec surface disappeared with operator cutover")
	}
}
