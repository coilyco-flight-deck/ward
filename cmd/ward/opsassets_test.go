package main

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestOpsAssetsEmbeddedSmoke ensures the embedded ops surface is present and
// still parseable after the bundle split.
func TestOpsAssetsEmbeddedSmoke(t *testing.T) {
	for _, embedPath := range []string{opsForgejoGuardfilePath, opsForgejoSpecLockPath} {
		got, err := bakedAssets.ReadFile(embedPath)
		if err != nil {
			t.Fatalf("read embedded %s: %v", embedPath, err)
		}
		if len(got) == 0 {
			t.Fatalf("embedded %s is empty", embedPath)
		}
	}
}

// TestOpsForgejoMounts asserts the embedded guardfile + spec lock build into a
// real command group, not the degraded error leaf.
func TestOpsForgejoMounts(t *testing.T) {
	dir := writeBundleFixture(t)
	t.Setenv(wardConfigRefEnv, "file://"+dir)
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
	dir := writeBundleFixture(t)
	t.Setenv(wardConfigRefEnv, "file://"+dir)
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

// TestOpsForgejoIssueCommentDeleteMounts pins ward#570's cleanup leaf: issue-comment
// carries both `list` and the added `delete` (a dropped spec-lock op regresses here).
func TestOpsForgejoIssueCommentDeleteMounts(t *testing.T) {
	dir := writeBundleFixture(t)
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	ic := commandNamed(forgejo.Commands, "issue-comment")
	if ic == nil {
		t.Fatalf("forgejo group missing issue-comment command; got %v", commandNames(forgejo.Commands))
	}
	for _, want := range []string{"list", "delete"} {
		if commandNamed(ic.Commands, want) == nil {
			t.Fatalf("issue-comment missing %q leaf; got %v", want, commandNames(ic.Commands))
		}
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
	dir := writeBundleFixture(t)
	t.Setenv(wardConfigRefEnv, "file://"+dir)
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
