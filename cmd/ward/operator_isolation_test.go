package main

import "testing"

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
