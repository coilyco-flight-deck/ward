package main

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// discardRunner builds a *Runner whose shell streams are discarded, for
// driving runGitCommit in tests (runGitCommit does not touch r.Audit).
func discardRunner() *Runner {
	return &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
}

// committedFiles returns the files touched by HEAD in repo.
func committedFiles(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "show", "--name-only", "--format=", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git show: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestRunGitCommitConcurrencySafety asserts a file staged in the shared
// index by "another session" never leaks into a named-path commit.
func TestRunGitCommitConcurrencySafety(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := newCommitRepo(t)
	writeFile(t, filepath.Join(repo, "a.txt"), "aaa\n")
	writeFile(t, filepath.Join(repo, "b.txt"), "bbb\n")
	git(t, repo, "add", "b.txt") // another session stages b.txt

	if err := discardRunner().runGitCommit(context.Background(),
		[]string{"-C", repo, "-m", "only a", "--", "a.txt"}); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	files := committedFiles(t, repo)
	if files != "a.txt" {
		t.Fatalf("HEAD touched %q, want only a.txt (b.txt leaked)", files)
	}
	// b.txt must still be staged in the shared index, untouched.
	status := gitStatus(t, repo)
	if !strings.Contains(status, "A  b.txt") {
		t.Fatalf("expected b.txt still staged, status:\n%s", status)
	}
}

// TestRunGitCommitNewFile commits a brand-new file (no HEAD entry), the
// empty-index case the private-index seeding must handle.
func TestRunGitCommitNewFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := newCommitRepo(t)
	writeFile(t, filepath.Join(repo, "new.txt"), "fresh\n")
	if err := discardRunner().runGitCommit(context.Background(),
		[]string{"-C", repo, "-m", "add new", "--", "new.txt"}); err != nil {
		t.Fatalf("commit new file failed: %v", err)
	}
	if got := committedFiles(t, repo); got != "new.txt" {
		t.Fatalf("HEAD touched %q, want new.txt", got)
	}
}

func TestRunGitCommitRefusals(t *testing.T) {
	r := discardRunner()
	ctx := context.Background()
	if err := r.runGitCommit(ctx, []string{"-m", "x"}); err == nil {
		t.Error("bare commit (no --) should be refused")
	}
	if err := r.runGitCommit(ctx, []string{"--"}); err == nil {
		t.Error("empty pathspec should be refused")
	}
	if err := r.runGitCommit(ctx, []string{"--", "a.txt"}); err == nil {
		t.Error("missing message source should be refused")
	}
	if err := r.runGitCommit(ctx, []string{"-m", "x", "-e", "--", "a.txt"}); err == nil {
		t.Error("editor flag should be refused")
	}
}

// newCommitRepo builds a git repo with one seed commit. No upstream needed
// (commit has no clean-tree gate).
func newCommitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "ward test")
	writeFile(t, filepath.Join(repo, "seed.txt"), "seed\n")
	git(t, repo, "add", "seed.txt")
	git(t, repo, "commit", "-qm", "seed")
	return repo
}

// gitStatus returns `git status --porcelain` for repo.
func gitStatus(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "status", "--porcelain")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

func TestHoistDashC(t *testing.T) {
	t.Run("leading -C is peeled", func(t *testing.T) {
		dir, rest := hoistDashC([]string{"-C", "/repo", "commit", "-m", "x"})
		if dir != "/repo" {
			t.Fatalf("dir = %q", dir)
		}
		if len(rest) != 3 || rest[0] != "commit" {
			t.Fatalf("rest = %v", rest)
		}
	})
	t.Run("no -C", func(t *testing.T) {
		dir, rest := hoistDashC([]string{"-m", "x", "--", "a"})
		if dir != "" || len(rest) != 4 {
			t.Fatalf("dir=%q rest=%v", dir, rest)
		}
	})
}

func TestSplitCommitArgs(t *testing.T) {
	t.Run("splits at --", func(t *testing.T) {
		flags, paths, err := splitCommitArgs([]string{"-m", "msg", "--", "a", "b"})
		if err != nil {
			t.Fatal(err)
		}
		if len(flags) != 2 || len(paths) != 2 || paths[0] != "a" {
			t.Fatalf("flags=%v paths=%v", flags, paths)
		}
	})
	t.Run("missing -- errors", func(t *testing.T) {
		if _, _, err := splitCommitArgs([]string{"-m", "msg"}); err == nil {
			t.Fatal("expected error without --")
		}
	})
	t.Run("empty paths after --", func(t *testing.T) {
		flags, paths, err := splitCommitArgs([]string{"-m", "msg", "--"})
		if err != nil || len(flags) != 2 || len(paths) != 0 {
			t.Fatalf("flags=%v paths=%v err=%v", flags, paths, err)
		}
	})
}

func TestHasMessageSource(t *testing.T) {
	yes := [][]string{{"-m"}, {"-F"}, {"-mmsg"}, {"-Ffile"}, {"--message"}, {"--message=x"}, {"--file=f"}}
	for _, f := range yes {
		if !hasMessageSource(f) {
			t.Errorf("hasMessageSource(%v) = false, want true", f)
		}
	}
	no := [][]string{{"-a"}, {"--amend"}, {}, {"--no-edit"}}
	for _, f := range no {
		if hasMessageSource(f) {
			t.Errorf("hasMessageSource(%v) = true, want false", f)
		}
	}
}

func TestHasEditorFlag(t *testing.T) {
	if !hasEditorFlag([]string{"-m", "x", "-e"}) {
		t.Error("-e not detected")
	}
	if !hasEditorFlag([]string{"--edit"}) {
		t.Error("--edit not detected")
	}
	if hasEditorFlag([]string{"-m", "x"}) {
		t.Error("false positive without editor flag")
	}
}

func TestGitArgv(t *testing.T) {
	t.Run("no dir", func(t *testing.T) {
		got := gitArgv("", "commit", []string{"-m", "x"}, []string{"a", "b"})
		want := []string{"commit", "-m", "x", "--", "a", "b"}
		assertArgv(t, got, want)
	})
	t.Run("with dir leads", func(t *testing.T) {
		got := gitArgv("/repo", "reset", []string{"-q", "HEAD"}, []string{"a"})
		want := []string{"-C", "/repo", "reset", "-q", "HEAD", "--", "a"}
		assertArgv(t, got, want)
	})
}

func TestGitVerbRewriter(t *testing.T) {
	t.Run("prepends verb", func(t *testing.T) {
		got := gitVerbRewriter("status")([]string{"--short"})
		assertArgv(t, got, []string{"status", "--short"})
	})
	t.Run("hoists -C before verb", func(t *testing.T) {
		got := gitVerbRewriter("log")([]string{"-C", "/r", "--oneline"})
		assertArgv(t, got, []string{"-C", "/r", "log", "--oneline"})
	})
}

func assertArgv(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv = %v, want %v", got, want)
		}
	}
}
