package main

import (
	"testing"

	"github.com/urfave/cli/v3"
)

// newWardKdlTestRoot mirrors the hand-written commands the real ward tree
// carries before the exec mount: the git + pkg-brew collisions and the ops group.
func newWardKdlTestRoot() *cli.Command {
	return &cli.Command{
		Name: "ward",
		Commands: []*cli.Command{
			{Name: "git"}, // hand-written, cmd/ward/git.go
			{Name: "pkg", Commands: []*cli.Command{{Name: "brew"}}},
			{Name: "ops", Commands: []*cli.Command{{Name: "forgejo"}}},
		},
	}
}

// TestMountWardKdlExecMountsNewSurfaces asserts the auto-discovery lights up the
// dark exec surfaces: `agents <tool>`, `docker`, and `ops {aws,kubectl}`.
func TestMountWardKdlExecMountsNewSurfaces(t *testing.T) {
	root := newWardKdlTestRoot()
	if err := mountWardKdlExec(root, leanRunner()); err != nil {
		t.Fatalf("mountWardKdlExec: %v", err)
	}

	agents := commandNamed(root.Commands, "agents")
	if agents == nil {
		t.Fatalf("ward agents group not mounted; got %v", commandNames(root.Commands))
	}
	for _, want := range []string{"claude", "codex", "ollama"} {
		if commandNamed(agents.Commands, want) == nil {
			t.Errorf("ward agents missing %q; got %v", want, commandNames(agents.Commands))
		}
	}

	if commandNamed(root.Commands, "docker") == nil {
		t.Errorf("ward docker group not mounted; got %v", commandNames(root.Commands))
	}

	ops := commandNamed(root.Commands, "ops")
	if ops == nil {
		t.Fatal("ops group vanished")
	}
	for _, want := range []string{"forgejo", "aws", "kubectl"} {
		if commandNamed(ops.Commands, want) == nil {
			t.Errorf("ward ops missing %q; got %v", want, commandNames(ops.Commands))
		}
	}
	// The exec aws guardfile's nested subcommands build through (sanity that
	// multi-segment grants mount, not just the top group).
	aws := commandNamed(ops.Commands, "aws")
	if aws == nil || commandNamed(aws.Commands, "sts") == nil {
		t.Errorf("ward ops aws did not mount its sts subtree; got %v", commandNames(aws.Commands))
	}
}

// TestMountWardKdlExecSkipsCollisions asserts a guardfile whose leaf collides
// with a hand-written command is skipped, never duplicated or overwritten.
func TestMountWardKdlExecSkipsCollisions(t *testing.T) {
	root := newWardKdlTestRoot()
	if err := mountWardKdlExec(root, leanRunner()); err != nil {
		t.Fatalf("mountWardKdlExec: %v", err)
	}

	// git: still the hand-written stub (no children grafted from the guardfile),
	// and exactly one `git` at root.
	if n := countNamed(root.Commands, "git"); n != 1 {
		t.Errorf("expected exactly one git command, got %d", n)
	}
	git := commandNamed(root.Commands, "git")
	if git == nil || len(git.Commands) != 0 {
		t.Errorf("hand-written git was overwritten by the guardfile mount; children = %v", commandNames(git.Commands))
	}

	// pkg brew: not duplicated.
	pkg := commandNamed(root.Commands, "pkg")
	if pkg == nil {
		t.Fatal("pkg group vanished")
	}
	if n := countNamed(pkg.Commands, "brew"); n != 1 {
		t.Errorf("expected exactly one pkg brew, got %d", n)
	}
}

func countNamed(cmds []*cli.Command, name string) int {
	n := 0
	for _, c := range cmds {
		if c.Name == name {
			n++
		}
	}
	return n
}
