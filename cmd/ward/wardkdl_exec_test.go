package main

import (
	"context"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
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
	for _, want := range []string{"forgejo", "forgejo-key", "aws", "kubectl"} {
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

// TestForgejoKeySealed asserts the `ops forgejo-key read` guardfile (ward#386)
// runs one frozen kubectl argv and refuses every caller pivot. See docs/ward-kdl/.
func TestForgejoKeySealed(t *testing.T) {
	gfBytes, err := execAssets.ReadFile(execAssetsDir + "/ward-kdl.forgejo-key.guardfile.kdl")
	if err != nil {
		t.Fatalf("read embedded forgejo-key guardfile: %v", err)
	}
	gf, err := execverb.Parse(gfBytes)
	if err != nil {
		t.Fatalf("parse forgejo-key guardfile: %v", err)
	}

	var ranArgv []string
	group, err := execverb.Build(execverb.Config{
		Guardfile: gf,
		Wrap:      func(s verb.Spec) cli.ActionFunc { return s.Action },
		Run: func(_ context.Context, bin string, argv, _ []string) error {
			ranArgv = append([]string{bin}, argv...)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("build forgejo-key group: %v", err)
	}
	root := &cli.Command{Name: "ward", Commands: []*cli.Command{{Name: "ops", Commands: []*cli.Command{group}}}}

	// The clean call runs exactly the sealed, single-key kubectl invocation.
	ranArgv = nil
	if err := root.Run(context.Background(), []string{"ward", "ops", "forgejo-key", "read"}); err != nil {
		t.Fatalf("clean `read` was refused: %v", err)
	}
	wantArgv := `kubectl get secret forgejo-runner-secrets -n forgejo -o go-template={{index .data "api-token" | base64decode}}`
	if got := strings.Join(ranArgv, " "); got != wantArgv {
		t.Errorf("sealed argv mismatch:\n got %q\nwant %q", got, wantArgv)
	}

	// Every pivot attempt is refused before any exec.
	pivots := map[string][]string{
		"trailing positional":   {"othersecret"},
		"trailing resource+ns":  {"secret", "other", "-A"},
		"namespace flag (-n)":   {"-n", "kube-system"},
		"output flag (-o)":      {"-o", "jsonpath={.data}"},
		"namespace flag (long)": {"--namespace=kube-system"},
	}
	for name, extra := range pivots {
		ranArgv = nil
		args := append([]string{"ward", "ops", "forgejo-key", "read"}, extra...)
		if err := root.Run(context.Background(), args); err == nil {
			t.Errorf("%s: expected denial, but call was allowed (ran %q)", name, strings.Join(ranArgv, " "))
		}
		if ranArgv != nil {
			t.Errorf("%s: kubectl was exec'd despite the seal: %q", name, strings.Join(ranArgv, " "))
		}
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
