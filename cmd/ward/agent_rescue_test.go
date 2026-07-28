package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRescueRepositoryVerifiedBundleAndRecovery(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	remote := t.TempDir()
	gitFixture(t, remote, "init", "--bare", "-b", "main")
	work := filepath.Join(t.TempDir(), "work")
	gitFixture(t, t.TempDir(), "init") // keeps the helper's cwd contract obvious
	gitFixture(t, filepath.Dir(work), "clone", remote, work)
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("rescued\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, work, "add", "feature.txt")
	gitFixture(t, work, "commit", "-m", "feature\n\ncloses #1515")
	// The first commit establishes main; the second is the unlanded rescue.
	gitFixture(t, work, "push", "origin", "HEAD:main")
	gitFixture(t, work, "fetch", "origin")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("rescued later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, work, "commit", "-am", "unlanded feature\n\ncloses #1515")

	r := gitRunner()
	r.Runner.Stderr = os.Stderr
	rep, ok, err := r.createRescueRepository(context.Background(), work, targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}, "engineer-codex-ward-1515")
	if err != nil || !ok {
		t.Fatalf("create rescue = (%+v, %t, %v)", rep, ok, err)
	}
	if rep.BundleSHA256 == "" || !strings.Contains(strings.Join(rep.Validation, " "), "sha256") {
		t.Fatalf("rescue validation missing: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(rescuesDir(), "engineer-codex-ward-1515", rep.Bundle)); err != nil {
		t.Fatalf("bundle not durable: %v", err)
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	gitFixture(t, filepath.Dir(fresh), "clone", remote, fresh)
	if err := r.Runner.Exec(context.Background(), "git", "-C", fresh, "fetch", filepath.Join(rescuesDir(), "engineer-codex-ward-1515", rep.Bundle), "HEAD"); err != nil {
		t.Fatalf("fetch rescue bundle: %v", err)
	}
	if err := r.Runner.Exec(context.Background(), "git", "-C", fresh, "merge", "--ff-only", "FETCH_HEAD"); err != nil {
		t.Fatalf("merge rescued head: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(fresh, "feature.txt"))
	if err != nil || strings.ReplaceAll(string(data), "\r\n", "\n") != "rescued later\n" {
		t.Fatalf("fresh recovery = %q, %v", data, err)
	}
}

func TestRescueQuarantineBroadDeletionAndGeneratedBinary(t *testing.T) {
	if q, _ := rescueQuarantine([]string{"D\ta.exe"}); !q {
		t.Fatal("generated binary must be quarantined")
	}
	var inv []string
	for i := 0; i < 20; i++ {
		inv = append(inv, "D\told/file")
	}
	if q, why := rescueQuarantine(inv); !q || !strings.Contains(why, "broad deletion") {
		t.Fatalf("broad deletion = (%t, %q), want quarantine", q, why)
	}
}

func TestExtractRescueGitDirRejectsTraversal(t *testing.T) {
	if err := extractRescueGitDir([]byte("not a tar"), t.TempDir()); err == nil {
		t.Fatal("invalid archive must fail closed")
	}
}
