package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
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

func TestOpsForgejoIssueListAllMounts(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	issue := commandNamed(forgejo.Commands, "issue")
	if issue == nil {
		t.Fatalf("forgejo group missing issue command; got %v", commandNames(forgejo.Commands))
	}
	if commandNamed(issue.Commands, "list-all") == nil {
		t.Fatalf("issue command missing list-all; got %v", commandNames(issue.Commands))
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

func commandNamed(cmds []*cli.Command, name string) *cli.Command {
	for _, cmd := range cmds {
		if cmd.Name == name {
			return cmd
		}
	}
	return nil
}

// TestRerootGroupToWard asserts the helper swaps the ward-kdl brand for ward,
// and is a no-op otherwise (ward#270).
func TestRerootGroupToWard(t *testing.T) {
	g := []string{"ward-kdl", "ops", "forgejo"}
	rerootGroupToWard(g)
	if g[0] != "ward" {
		t.Errorf("group[0] = %q, want %q", g[0], "ward")
	}
	already := []string{"ward", "ops", "forgejo"}
	rerootGroupToWard(already)
	if already[0] != "ward" {
		t.Errorf("no-op case mutated group[0] to %q", already[0])
	}
	rerootGroupToWard(nil) // must not panic on an empty group
}

// TestOpsForgejoNamespaceRerooted asserts the in-binary forgejo group mounts
// under ward's own brand, not the standalone ward-kdl binary's (ward#270).
func TestOpsForgejoNamespaceRerooted(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	if !strings.Contains(forgejo.Usage, "ward ops forgejo") {
		t.Errorf("forgejo group Usage = %q, want it to name `ward ops forgejo`", forgejo.Usage)
	}
	if strings.Contains(forgejo.Usage, "ward-kdl") {
		t.Errorf("forgejo group Usage = %q still carries the ward-kdl brand", forgejo.Usage)
	}
}

func commandNames(cmds []*cli.Command) []string {
	names := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		names = append(names, cmd.Name)
	}
	return names
}
