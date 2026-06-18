package main

import (
	"bytes"
	"os"
	"testing"
)

// canonicalOpsAssets pairs each embedded copy with the ward-kdl source it must
// mirror (the embed copies exist only because go:embed can't reach a sibling dir).
var canonicalOpsAssets = map[string]string{
	opsForgejoGuardfilePath: "../ward-kdl/ward-kdl.forgejo.guardfile.kdl",
	opsForgejoSpecLockPath:  "../ward-kdl/forgejo.swagger.lock.json",
}

// TestOpsAssetsMatchWardKDL fails when an embedded ops asset drifts from its
// canonical ward-kdl source. See docs/ops-forgejo.md.
func TestOpsAssetsMatchWardKDL(t *testing.T) {
	for embedPath, canonical := range canonicalOpsAssets {
		want, err := os.ReadFile(canonical)
		if err != nil {
			t.Fatalf("read canonical %s: %v", canonical, err)
		}
		got, err := opsAssets.ReadFile(embedPath)
		if err != nil {
			t.Fatalf("read embedded %s: %v", embedPath, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("embedded %s has drifted from %s; resync with `make build-ward-kdl` "+
				"(or copy the file) so ward embeds the current guardfile/spec", embedPath, canonical)
		}
	}
}

// TestOpsForgejoMounts asserts the embedded guardfile + spec lock build into a
// real command group, not the degraded error leaf.
func TestOpsForgejoMounts(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	if forgejo.Name != "forgejo" {
		t.Errorf("group name = %q, want %q", forgejo.Name, "forgejo")
	}
	if len(forgejo.Commands) == 0 {
		t.Fatal("forgejo group mounted no resource subcommands")
	}
}

// TestOpsCommandShape asserts the umbrella mounts forgejo under `ops`, the shape
// main.go registers.
func TestOpsCommandShape(t *testing.T) {
	cmd := opsCommand()
	if cmd.Name != "ops" {
		t.Fatalf("command name = %q, want %q", cmd.Name, "ops")
	}
	var found bool
	for _, sub := range cmd.Commands {
		if sub.Name == "forgejo" {
			found = true
		}
	}
	if !found {
		t.Error("ops umbrella is missing the forgejo group")
	}
}
