package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/shell"
)

func TestResolveGitCommitIdentityPreservesNativeSources(t *testing.T) {
	tests := []struct {
		name      string
		configure func(t *testing.T, runner *shell.Runner, repo string)
		wantName  string
		wantEmail string
	}{
		{
			name: "repository local",
			configure: func(t *testing.T, runner *shell.Runner, repo string) {
				t.Helper()
				runIdentityGit(t, runner, "-C", repo, "config", "user.name", "Local Author")
				runIdentityGit(t, runner, "-C", repo, "config", "user.email", "local@fixtures.invalid")
			},
			wantName: "Local Author", wantEmail: "local@fixtures.invalid",
		},
		{
			name: "global",
			configure: func(t *testing.T, runner *shell.Runner, _ string) {
				t.Helper()
				runIdentityGit(t, runner, "config", "--global", "user.name", "Global Author")
				runIdentityGit(t, runner, "config", "--global", "user.email", "global@fixtures.invalid")
			},
			wantName: "Global Author", wantEmail: "global@fixtures.invalid",
		},
		{
			name: "Git environment",
			configure: func(_ *testing.T, runner *shell.Runner, _ string) {
				runner.Env = append(runner.Env,
					"GIT_AUTHOR_NAME=Environment Author",
					"GIT_AUTHOR_EMAIL=author-env@fixtures.invalid",
					"GIT_COMMITTER_NAME=Environment Committer",
					"GIT_COMMITTER_EMAIL=committer-env@fixtures.invalid",
				)
			},
			wantName: "Environment Author", wantEmail: "author-env@fixtures.invalid",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, runner := newGitIdentityFixture(t)
			tc.configure(t, runner, repo)
			got, err := resolveGitCommitIdentity(t.Context(), runner, repo, nil)
			if err != nil {
				t.Fatalf("resolve identity: %v", err)
			}
			if got["GIT_AUTHOR_NAME"] != tc.wantName || got["GIT_AUTHOR_EMAIL"] != tc.wantEmail {
				t.Fatalf("author = %q <%s>, want %q <%s>", got["GIT_AUTHOR_NAME"], got["GIT_AUTHOR_EMAIL"], tc.wantName, tc.wantEmail)
			}
			if tc.name != "Git environment" && (got["GIT_COMMITTER_NAME"] != tc.wantName || got["GIT_COMMITTER_EMAIL"] != tc.wantEmail) {
				t.Fatalf("committer = %q <%s>, want %q <%s>", got["GIT_COMMITTER_NAME"], got["GIT_COMMITTER_EMAIL"], tc.wantName, tc.wantEmail)
			}
			if tc.name == "Git environment" && (got["GIT_COMMITTER_NAME"] != "Environment Committer" || got["GIT_COMMITTER_EMAIL"] != "committer-env@fixtures.invalid") {
				t.Fatalf("environment committer not preserved: %#v", got)
			}
		})
	}
}

func TestResolveGitCommitIdentityUsesExplicitWardFallback(t *testing.T) {
	repo, runner := newGitIdentityFixture(t)
	got, err := resolveGitCommitIdentity(t.Context(), runner, repo, map[string]string{
		"WARD_GIT_NAME": "Explicit Ward Author", "WARD_GIT_EMAIL": "ward@fixtures.invalid",
	})
	if err != nil {
		t.Fatalf("resolve WARD_GIT fallback: %v", err)
	}
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		if got[key] != "Explicit Ward Author" {
			t.Fatalf("%s = %q", key, got[key])
		}
	}
	for _, key := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		if got[key] != "ward@fixtures.invalid" {
			t.Fatalf("%s = %q", key, got[key])
		}
	}
}

func TestResolveGitCommitIdentityFailsClosedForMissingAndPartialIdentity(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want string
	}{
		{name: "none", want: "author name/email and committer name/email"},
		{name: "author only", env: []string{"GIT_AUTHOR_NAME=Author", "GIT_AUTHOR_EMAIL=author@fixtures.invalid"}, want: "committer name/email"},
		{name: "committer only", env: []string{"GIT_COMMITTER_NAME=Committer", "GIT_COMMITTER_EMAIL=committer@fixtures.invalid"}, want: "author name/email"},
		{name: "partial author", env: []string{"GIT_AUTHOR_NAME=Author"}, want: "author name/email"},
		{name: "partial committer", env: []string{"GIT_COMMITTER_EMAIL=committer@fixtures.invalid"}, want: "committer name/email"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, runner := newGitIdentityFixture(t)
			runner.Env = append(runner.Env, tc.env...)
			_, err := resolveGitCommitIdentity(t.Context(), runner, repo, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want missing %q", err, tc.want)
			}
		})
	}
}

func TestRunGitCommitMissingIdentityLeavesHeadAndIndexUnchanged(t *testing.T) {
	repo, shellRunner := newGitIdentityFixture(t)
	file := filepath.Join(repo, "work.txt")
	if err := os.WriteFile(file, []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIdentityGit(t, shellRunner, "-C", repo, "add", "work.txt")
	indexPath := filepath.Join(repo, ".git", "index")
	beforeIndex, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{Runner: shellRunner}
	err = r.runGitCommit(context.Background(), []string{"-C", repo, "-m", "must fail", "--", "work.txt"})
	if err == nil || !strings.Contains(err.Error(), "git commit identity is missing") {
		t.Fatalf("runGitCommit error = %v", err)
	}
	if _, err := shellRunner.Capture(t.Context(), "git", "-C", repo, "rev-parse", "--verify", "HEAD"); err == nil {
		t.Fatal("missing identity unexpectedly created HEAD")
	}
	afterIndex, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeIndex, afterIndex) {
		t.Fatal("missing identity mutated the shared Git index")
	}
}

func TestReaperMissingIdentityDoesNotStageOrCommitResidualWork(t *testing.T) {
	repo, shellRunner := newGitIdentityFixture(t)
	file := filepath.Join(repo, "residual.txt")
	if err := os.WriteFile(file, []byte("residual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := shellRunner.Capture(t.Context(), "git", "-C", repo, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{Runner: shellRunner}
	if got := r.captureAndCommitResidualRepo(t.Context(), repo, "codex", "owner/repo"); strings.TrimSpace(got) == "" {
		t.Fatal("reaper did not report residual work")
	}
	after, err := shellRunner.Capture(t.Context(), "git", "-C", repo, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("reaper mutated the index without identity: before=%q after=%q", before, after)
	}
	if _, err := shellRunner.Capture(t.Context(), "git", "-C", repo, "rev-parse", "--verify", "HEAD"); err == nil {
		t.Fatal("reaper created a commit without identity")
	}
}

func TestProjectEngineerGitIdentityCoversEveryWorkflow(t *testing.T) {
	for _, workflow := range []workflowMode{workflowDirectToMain, workflowPullRequest, workflowPullRequestAndMerge, workflowRemoteBranchOnly} {
		t.Run(string(workflow), func(t *testing.T) {
			repo, runner := newGitIdentityFixture(t)
			plan := upPlan{Workflow: workflow, ConfigEnv: map[string]string{
				"WARD_GIT_NAME": "Workflow Author", "WARD_GIT_EMAIL": "workflow@fixtures.invalid",
			}}
			if err := projectEngineerGitIdentity(t.Context(), runner, &plan, repo); err != nil {
				t.Fatalf("project identity: %v", err)
			}
			if plan.ConfigEnv["GIT_AUTHOR_NAME"] != "Workflow Author" || plan.ConfigEnv["GIT_COMMITTER_EMAIL"] != "workflow@fixtures.invalid" {
				t.Fatalf("workflow %s identity projection = %#v", workflow, plan.ConfigEnv)
			}
		})
	}
}

func TestApplyWardGitIdentityFallbackNeverOverwritesNativeEnvironment(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Native Author")
	t.Setenv("GIT_AUTHOR_EMAIL", "")
	t.Setenv("GIT_COMMITTER_NAME", "")
	t.Setenv("GIT_COMMITTER_EMAIL", "")
	applyWardGitIdentityFallback("Ward Fallback", "ward@fixtures.invalid")
	if got := os.Getenv("GIT_AUTHOR_NAME"); got != "Native Author" {
		t.Fatalf("native author name overwritten with %q", got)
	}
	if got := os.Getenv("GIT_AUTHOR_EMAIL"); got != "ward@fixtures.invalid" {
		t.Fatalf("missing author email not filled: %q", got)
	}
	if got := os.Getenv("GIT_COMMITTER_NAME"); got != "Ward Fallback" {
		t.Fatalf("missing committer name not filled: %q", got)
	}
}

func TestGitSystemPolicyHasNoIdentityWrites(t *testing.T) {
	joined := make([]string, 0)
	for _, args := range gitSystemPolicyArgs() {
		joined = append(joined, strings.Join(args, " "))
	}
	all := strings.Join(joined, "\n")
	if !strings.Contains(all, "user.useConfigOnly true") {
		t.Fatalf("system policy missing useConfigOnly:\n%s", all)
	}
	for _, forbidden := range []string{"user.name", "user.email"} {
		if strings.Contains(all, forbidden) {
			t.Fatalf("system policy writes %s:\n%s", forbidden, all)
		}
	}
}

func TestGitUseConfigOnlyArgv(t *testing.T) {
	got := strings.Join(gitUseConfigOnlyArgv("/repo", "commit", "-m", "message"), " ")
	if got != "-C /repo -c user.useConfigOnly=true commit -m message" {
		t.Fatalf("Git argv = %q", got)
	}
}

func newGitIdentityFixture(t *testing.T) (string, *shell.Runner) {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	for _, key := range []string{
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE",
		"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE", "EMAIL",
	} {
		value, present := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	runner := &shell.Runner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Env: []string{
			"HOME=" + home,
			"GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig"),
			"GIT_CONFIG_NOSYSTEM=1",
		},
	}
	runIdentityGit(t, runner, "init", "-q", repo)
	return repo, runner
}

func runIdentityGit(t *testing.T, runner *shell.Runner, args ...string) {
	t.Helper()
	if err := runner.Exec(t.Context(), "git", args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}
