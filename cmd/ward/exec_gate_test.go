package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/gittree"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
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

func TestFormatExecGateRefusal(t *testing.T) {
	cases := []struct {
		name  string
		state *gittree.State
		verb  string
		keep  []string
		drop  []string
	}{
		{
			name:  "dirty tree keeps clean-tree guidance",
			state: &gittree.State{Reason: "working tree is dirty", Recovery: "  git commit\n", Status: " M README\n"},
			verb:  "repo.test",
			keep:  []string{"Repo verbs require a clean tree", "--audit-override-dirty"},
		},
		{
			name:  "missing upstream gets upstream guidance",
			state: &gittree.State{Reason: "branch \"main\" has no upstream", Branch: "main"},
			verb:  "repo.test",
			keep:  []string{"branch with an upstream", "git push -u origin main"},
			drop:  []string{"clean tree", "--audit-override-dirty"},
		},
		{
			name:  "behind upstream gets sync guidance",
			state: &gittree.State{Reason: "1 commits behind origin/main"},
			verb:  "repo.test",
			keep:  []string{"synced branch", "git pull --ff-only"},
			drop:  []string{"clean tree", "--audit-override-dirty"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatExecGateRefusal(tc.state, tc.verb)
			for _, want := range tc.keep {
				if !strings.Contains(got, want) {
					t.Fatalf("formatExecGateRefusal missing %q; got:\n%s", want, got)
				}
			}
			for _, notWant := range tc.drop {
				if strings.Contains(got, notWant) {
					t.Fatalf("formatExecGateRefusal unexpectedly contained %q; got:\n%s", notWant, got)
				}
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
	// Each subtest owns its CI fixture. Do not let the enclosing runner's
	// pull-request metadata reclassify ordinary local repositories.
	clearForgejoCIEnv(t)

	t.Run("clean synced tree passes", func(t *testing.T) {
		repo := newSyncedRepo(t)
		state, ci, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err != nil {
			t.Fatalf("clean tree refused: %v", err)
		}
		if used {
			t.Fatalf("override should be false on a clean pass")
		}
		if ci != nil {
			t.Fatalf("named branch unexpectedly captured CI context: %+v", ci)
		}
		if !state.Clean {
			t.Fatalf("expected clean state, got %+v", state)
		}
	})

	t.Run("dirt outside ward.yaml passes and captures status", func(t *testing.T) {
		repo := newSyncedRepo(t)
		writeFile(t, filepath.Join(repo, "scratch.txt"), "dirty\n")
		state, ci, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err != nil {
			t.Fatalf("dirt outside config refused: %v", err)
		}
		if used {
			t.Fatalf("override should be false when dirt is outside config")
		}
		if ci != nil {
			t.Fatalf("named dirty branch unexpectedly captured CI context: %+v", ci)
		}
		if state.Status == "" {
			t.Fatalf("expected captured working-tree status for the audit row")
		}
	})

	t.Run("dirty ward.yaml refuses without override", func(t *testing.T) {
		repo := newSyncedRepo(t)
		writeFile(t, filepath.Join(repo, ".ward", "ward.yaml"), "commands: {}\n# dirty\n")
		_, _, _, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err == nil {
			t.Fatalf("expected refusal when ward.yaml is dirty")
		}
	})

	t.Run("override bypasses a dirty ward.yaml", func(t *testing.T) {
		repo := newSyncedRepo(t)
		writeFile(t, filepath.Join(repo, ".ward", "ward.yaml"), "commands: {}\n# dirty\n")
		state, ci, used, err := runExecGate(rootCmd(true), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err != nil {
			t.Fatalf("override should bypass the gate: %v", err)
		}
		if !used {
			t.Fatalf("expected overrideUsed=true when the gate was bypassed")
		}
		if ci != nil {
			t.Fatalf("override unexpectedly captured CI context: %+v", ci)
		}
		if state.Status == "" {
			t.Fatalf("expected captured working-tree status under override")
		}
	})

	t.Run("read-only surface-check skips upstream fetch", func(t *testing.T) {
		repo := t.TempDir()
		git(t, repo, "init", "-b", "main", ".")
		git(t, repo, "config", "user.email", "test@example.com")
		git(t, repo, "config", "user.name", "ward test")
		if err := os.MkdirAll(filepath.Join(repo, ".ward"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(repo, ".ward", "ward.yaml"), "commands: {}\n")
		git(t, repo, "add", ".")
		git(t, repo, "commit", "-m", "seed")

		state, ci, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.surface-check", true)
		if err != nil {
			t.Fatalf("read-only surface-check refused: %v", err)
		}
		if used {
			t.Fatalf("override should be false on the read-only pass")
		}
		if ci != nil {
			t.Fatalf("read-only named branch unexpectedly captured CI context: %+v", ci)
		}
		if !state.Clean {
			t.Fatalf("expected clean read-only state, got %+v", state)
		}
	})

	t.Run("detached Forgejo pull-request merge passes with audited attribution", func(t *testing.T) {
		repo, mergeSHA := newDetachedForgejoPRRepo(t)
		state, ci, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err != nil {
			t.Fatalf("validated Forgejo checkout refused: %v", err)
		}
		if used {
			t.Fatal("detached CI pass must not use the dirty-tree override")
		}
		if !state.Clean || state.Branch != "HEAD" {
			t.Fatalf("detached CI state = %+v, want clean detached pass", state)
		}
		if ci == nil || ci.Provider != "forgejo-actions" || ci.PullRequest != "42" || ci.HeadSHA != mergeSHA || ci.RunID != "1234" {
			t.Fatalf("CI context = %+v, want immutable PR and run attribution", ci)
		}
		rec := &audit.Record{}
		applyExecGateAudit(rec, state, ci, used)
		if rec.CI != ci {
			t.Fatalf("audit CI context = %+v, want validated context", rec.CI)
		}
	})

	t.Run("named Forgejo pull-request merge passes with audited attribution", func(t *testing.T) {
		repo, mergeSHA := newDetachedForgejoPRRepo(t)
		git(t, repo, "switch", "-c", "ward-ci")
		git(t, repo, "branch", "--set-upstream-to=origin/main")
		state, ci, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err != nil {
			t.Fatalf("named Forgejo checkout refused: %v", err)
		}
		if used {
			t.Fatal("named CI pass must not use the dirty-tree override")
		}
		if !state.Clean || state.Branch != "ward-ci" {
			t.Fatalf("named CI state = %+v, want clean ward-ci pass", state)
		}
		if ci == nil || ci.Provider != "forgejo-actions" || ci.PullRequest != "42" || ci.HeadSHA != mergeSHA || ci.RunID != "1234" {
			t.Fatalf("CI context = %+v, want immutable PR and run attribution", ci)
		}
	})

	t.Run("exec leaf runs and serializes detached CI attribution", func(t *testing.T) {
		repo, mergeSHA := newDetachedForgejoPRRepo(t)
		auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
		runner := &Runner{
			Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard, Stdin: strings.NewReader("")},
			Audit:  audit.NewWriter(auditPath),
		}
		cfg := &repocfg.Config{Path: filepath.Join(repo, ".ward", "ward.yaml")}
		command := repocfg.Command{Name: "test", Argv: []string{"true"}}
		if err := runner.runExecLeaf(context.Background(), rootCmd(false), cfg, command); err != nil {
			t.Fatalf("runExecLeaf: %v", err)
		}
		body, err := os.ReadFile(auditPath)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := audit.ReadAll(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Decision != audit.DecisionAccept || rows[0].CI == nil || rows[0].CI.HeadSHA != mergeSHA {
			t.Fatalf("audit rows = %+v, want one accepted row with detached HEAD attribution", rows)
		}
	})

	t.Run("local detached checkout refuses even with override", func(t *testing.T) {
		repo := newSyncedRepo(t)
		git(t, repo, "checkout", "--detach", "HEAD")
		clearForgejoCIEnv(t)
		_, _, _, err := runExecGate(rootCmd(true), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err == nil || !strings.Contains(err.Error(), `CI must equal "true"`) {
			t.Fatalf("local detached checkout error = %v, want missing CI evidence", err)
		}
	})

	t.Run("detached Forgejo checkout refuses missing metadata", func(t *testing.T) {
		repo, _ := newDetachedForgejoPRRepo(t)
		t.Setenv("GITHUB_RUN_ID", "")
		_, _, _, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err == nil || !strings.Contains(err.Error(), "GITHUB_RUN_ID is missing") {
			t.Fatalf("missing metadata error = %v", err)
		}
	})

	t.Run("detached Forgejo checkout refuses inconsistent metadata", func(t *testing.T) {
		repo, _ := newDetachedForgejoPRRepo(t)
		t.Setenv("GITHUB_HEAD_REF", "different-head")
		_, _, _, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err == nil || !strings.Contains(err.Error(), "event base/head refs") {
			t.Fatalf("inconsistent metadata error = %v", err)
		}
	})

	t.Run("detached Forgejo checkout refuses inconsistent merge parents", func(t *testing.T) {
		repo, _ := newDetachedForgejoPRRepo(t)
		eventPath := os.Getenv("GITHUB_EVENT_PATH")
		event, err := readForgejoPREvent(eventPath)
		if err != nil {
			t.Fatal(err)
		}
		event.PullRequest.Head.SHA = strings.Repeat("a", 40)
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, eventPath, string(body))
		_, _, _, err = runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err == nil || !strings.Contains(err.Error(), "parents do not match") {
			t.Fatalf("inconsistent parent error = %v", err)
		}
	})

	t.Run("detached Forgejo checkout must stay clean", func(t *testing.T) {
		repo, _ := newDetachedForgejoPRRepo(t)
		writeFile(t, filepath.Join(repo, "scratch.txt"), "dirty\n")
		_, _, _, err := runExecGate(rootCmd(true), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err == nil || !strings.Contains(err.Error(), "dirty working tree") {
			t.Fatalf("dirty detached checkout error = %v", err)
		}
	})

	t.Run("ordinary branch push ignores detached CI metadata", func(t *testing.T) {
		repo := newSyncedRepo(t)
		clearForgejoCIEnv(t)
		t.Setenv("FORGEJO_ACTIONS", "true")
		t.Setenv("GITHUB_SHA", "inconsistent-on-purpose")
		state, ci, used, err := runExecGate(rootCmd(false), repo, filepath.Join(repo, ".ward", "ward.yaml"), "repo.test", false)
		if err != nil || used || ci != nil || !state.Clean {
			t.Fatalf("named branch result state=%+v ci=%+v override=%v err=%v", state, ci, used, err)
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

func newDetachedForgejoPRRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := newSyncedRepo(t)
	baseRef := gitText(t, repo, "branch", "--show-current")
	baseSHA := gitText(t, repo, "rev-parse", "HEAD")
	// Keep this fixture independent of whether push -u materialized the
	// remote-tracking ref when it seeded the initially empty bare remote.
	git(t, repo, "update-ref", "refs/remotes/origin/main", baseSHA)
	git(t, repo, "checkout", "-b", "feature/ci")
	writeFile(t, filepath.Join(repo, "feature.txt"), "feature\n")
	git(t, repo, "add", "feature.txt")
	git(t, repo, "commit", "-m", "feature")
	headSHA := gitText(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", baseRef)
	git(t, repo, "merge", "--no-ff", "feature/ci", "-m", "synthetic pull-request merge")
	mergeSHA := gitText(t, repo, "rev-parse", "HEAD")
	git(t, repo, "remote", "set-url", "origin", "https://forgejo.example/owner/repo.git")
	git(t, repo, "checkout", "--detach", mergeSHA)

	event := map[string]any{
		"repository": map[string]any{"full_name": "owner/repo"},
		"sender":     map[string]any{"login": "automation"},
		"pull_request": map[string]any{
			"base": map[string]any{"ref": baseRef, "sha": baseSHA},
			"head": map[string]any{"ref": "feature/ci", "sha": headSHA},
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(t.TempDir(), "event.json")
	writeFile(t, eventPath, string(body))

	for key, value := range map[string]string{
		"CI":                 "true",
		"FORGEJO_ACTIONS":    "true",
		"GITHUB_ACTIONS":     "true",
		"GITHUB_ACTOR":       "automation",
		"GITHUB_BASE_REF":    baseRef,
		"GITHUB_EVENT_NAME":  "pull_request",
		"GITHUB_EVENT_PATH":  eventPath,
		"GITHUB_HEAD_REF":    "feature/ci",
		"GITHUB_JOB":         "unit",
		"GITHUB_REF":         "refs/pull/42/merge",
		"GITHUB_REPOSITORY":  "owner/repo",
		"GITHUB_RUN_ATTEMPT": "2",
		"GITHUB_RUN_ID":      "1234",
		"GITHUB_RUN_NUMBER":  "56",
		"GITHUB_SERVER_URL":  "https://forgejo.example",
		"GITHUB_SHA":         mergeSHA,
		"GITHUB_WORKFLOW":    "test",
		"GITHUB_WORKSPACE":   repo,
	} {
		t.Setenv(key, value)
	}
	return repo, mergeSHA
}

func clearForgejoCIEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CI", "FORGEJO_ACTIONS", "GITHUB_ACTIONS", "GITHUB_ACTOR", "GITHUB_BASE_REF",
		"GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH", "GITHUB_HEAD_REF", "GITHUB_JOB", "GITHUB_REF",
		"GITHUB_REPOSITORY", "GITHUB_RUN_ATTEMPT", "GITHUB_RUN_ID", "GITHUB_RUN_NUMBER",
		"GITHUB_SERVER_URL", "GITHUB_SHA", "GITHUB_WORKFLOW", "GITHUB_WORKSPACE",
	} {
		t.Setenv(key, "")
	}
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
