package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/gittree"
	"github.com/urfave/cli/v3"
)

func TestDirtIsOutsideWardConfig(t *testing.T) {
	const root = "/repo"
	cfg := filepath.Join(root, ".ward", "ward.yaml")

	cases := []struct {
		name  string
		state *gittree.State
		want  bool
	}{
		{
			name:  "clean-tree state never qualifies",
			state: &gittree.State{Reason: ""},
			want:  false,
		},
		{
			name:  "upstream refusal still gates even if config committed",
			state: &gittree.State{Reason: "branch \"main\" has no upstream"},
			want:  false,
		},
		{
			name:  "dirty config itself gates",
			state: &gittree.State{Reason: "working tree is dirty", DirtyPaths: []string{".ward/ward.yaml"}},
			want:  false,
		},
		{
			name:  "dirt outside config does not gate",
			state: &gittree.State{Reason: "working tree is dirty", DirtyPaths: []string{"cmd/ward/exec.go"}},
			want:  true,
		},
		{
			name:  "mixed dirt including config gates",
			state: &gittree.State{Reason: "working tree is dirty", DirtyPaths: []string{"cmd/ward/exec.go", ".ward/ward.yaml"}},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dirtIsOutsideWardConfig(tc.state, root, cfg)
			if got != tc.want {
				t.Fatalf("dirtIsOutsideWardConfig(%+v) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// TestRunExecGateIntegration drives runExecGate against real git working
// trees, exercising each gate arm through gittree.CheckClean.
func TestRunExecGateIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	t.Run("clean synced tree passes", func(t *testing.T) {
		repo := newSyncedRepo(t)
		state, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test")
		if err != nil {
			t.Fatalf("clean tree refused: %v", err)
		}
		if used {
			t.Fatalf("override should be false on a clean pass")
		}
		if !state.Clean {
			t.Fatalf("expected clean state, got %+v", state)
		}
	})

	t.Run("dirt outside ward.yaml passes and captures status", func(t *testing.T) {
		repo := newSyncedRepo(t)
		writeFile(t, filepath.Join(repo, "scratch.txt"), "dirty\n")
		state, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test")
		if err != nil {
			t.Fatalf("dirt outside config refused: %v", err)
		}
		if used {
			t.Fatalf("override should be false when dirt is outside config")
		}
		if state.Status == "" {
			t.Fatalf("expected captured working-tree status for the audit row")
		}
	})

	t.Run("dirty ward.yaml refuses without override", func(t *testing.T) {
		repo := newSyncedRepo(t)
		writeFile(t, filepath.Join(repo, ".ward", "ward.yaml"), "commands: {}\n# dirty\n")
		_, _, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test")
		if err == nil {
			t.Fatalf("expected refusal when ward.yaml is dirty")
		}
	})

	t.Run("override bypasses a dirty ward.yaml", func(t *testing.T) {
		repo := newSyncedRepo(t)
		writeFile(t, filepath.Join(repo, ".ward", "ward.yaml"), "commands: {}\n# dirty\n")
		state, used, err := runExecGate(rootCmd(true), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test")
		if err != nil {
			t.Fatalf("override should bypass the gate: %v", err)
		}
		if !used {
			t.Fatalf("expected overrideUsed=true when the gate was bypassed")
		}
		if state.Status == "" {
			t.Fatalf("expected captured working-tree status under override")
		}
	})
}

// newSyncedRepo builds a git repo with a committed .ward/ward.yaml and a
// local upstream so gittree's synced-branch check passes.
func newSyncedRepo(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	work := filepath.Join(base, "work")

	git(t, base, "init", "--bare", remote)
	git(t, base, "clone", remote, work)
	git(t, work, "config", "user.email", "test@example.com")
	git(t, work, "config", "user.name", "ward test")

	if err := os.MkdirAll(filepath.Join(work, ".ward"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, ".ward", "ward.yaml"), "commands: {}\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "seed")
	git(t, work, "push", "-u", "origin", "HEAD")
	return work
}

// rootCmd returns a parsed root *cli.Command carrying audit-override-dirty
// set to override, for runExecGate's c.Root().Bool lookup.
func rootCmd(override bool) *cli.Command {
	var captured *cli.Command
	app := &cli.Command{
		Name:  "ward",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "audit-override-dirty"}},
		Action: func(_ context.Context, c *cli.Command) error {
			captured = c
			return nil
		},
	}
	args := []string{"ward"}
	if override {
		args = append(args, "--audit-override-dirty")
	}
	_ = app.Run(context.Background(), args)
	return captured
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
