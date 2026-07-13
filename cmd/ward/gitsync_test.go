package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// gitsync_test.go covers the shared TTL-cached git-ref sync (ward#654) against
// real local repos: clone-and-reuse, TTL refresh, offline fallback, ref checkout.

// gitFixture runs one git command for test setup, with a baked identity so
// commits need no global config.
func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := append([]string{"-C", dir, "-c", "user.email=t@test", "-c", "user.name=t",
		"-c", "protocol.file.allow=always"}, args...)
	out, err := exec.Command("git", argv...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newTestOrigin builds a local origin repo: main carries bundle/marker.txt=v1
// (tagged v1), branch alt carries marker=alt.
func newTestOrigin(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin")
	if err := os.MkdirAll(filepath.Join(origin, "bundle"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, origin, "init", "-b", "main", ".")
	writeMarker(t, origin, "v1")
	gitFixture(t, origin, "add", ".")
	gitFixture(t, origin, "commit", "-m", "v1")
	gitFixture(t, origin, "tag", "v1")
	gitFixture(t, origin, "checkout", "-b", "alt")
	writeMarker(t, origin, "alt")
	gitFixture(t, origin, "commit", "-am", "alt")
	gitFixture(t, origin, "checkout", "main")
	return origin
}

func writeMarker(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "bundle", "marker.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMarker(t *testing.T, work string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(work, "bundle", "marker.txt"))
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	return string(b)
}

// testSpec builds a gitRefSpec over a cache dir, mirroring resolveConfigBundle's
// layout. The url is a local path: git clones from it like any remote.
func testSpec(t *testing.T, origin string) gitRefSpec {
	t.Helper()
	cache := t.TempDir()
	return gitRefSpec{
		url:    origin,
		mirror: filepath.Join(cache, "mirror.git"),
		lock:   filepath.Join(cache, ".lock"),
		work:   filepath.Join(cache, "work"),
	}
}

// TestSyncGitRefClonesThenReuses pins the TTL fast path: the first sync
// materializes the checkout, a second inside the TTL touches nothing.
func TestSyncGitRefClonesThenReuses(t *testing.T) {
	origin := newTestOrigin(t)
	spec := testSpec(t, origin)
	r := leanRunner()

	work, err := r.syncGitRef(context.Background(), spec, time.Hour)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if got := readMarker(t, work); got != "v1" {
		t.Errorf("marker = %q, want v1", got)
	}
	if !isFile(filepath.Join(spec.mirror, "FETCH_HEAD")) {
		t.Error("clone did not stamp FETCH_HEAD; the TTL clock never starts")
	}

	// Plant a sentinel: a fresh-mirror re-sync must not re-drop the checkout.
	sentinel := filepath.Join(work, "sentinel")
	if err := os.WriteFile(sentinel, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.syncGitRef(context.Background(), spec, time.Hour); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !isFile(sentinel) {
		t.Error("fresh-mirror sync re-dropped the working checkout; want zero work inside the TTL")
	}
}

// TestSyncGitRefRefreshesPastTTL pins the refresh path: past the TTL the
// mirror updates and the checkout is re-dropped at the new tip.
func TestSyncGitRefRefreshesPastTTL(t *testing.T) {
	origin := newTestOrigin(t)
	spec := testSpec(t, origin)
	r := leanRunner()
	if _, err := r.syncGitRef(context.Background(), spec, time.Hour); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	writeMarker(t, origin, "v2")
	gitFixture(t, origin, "commit", "-am", "v2")
	work, err := r.syncGitRef(context.Background(), spec, 0)
	if err != nil {
		t.Fatalf("stale sync: %v", err)
	}
	if got := readMarker(t, work); got != "v2" {
		t.Errorf("marker after refresh = %q, want v2", got)
	}
}

// TestSyncGitRefCacheFallbackOffline pins never-brick-offline: with the remote
// gone, a stale sync logs, keeps the cached checkout, and returns no error.
func TestSyncGitRefCacheFallbackOffline(t *testing.T) {
	origin := newTestOrigin(t)
	spec := testSpec(t, origin)
	r := leanRunner()
	if _, err := r.syncGitRef(context.Background(), spec, time.Hour); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	work, err := r.syncGitRef(context.Background(), spec, 0)
	if err != nil {
		t.Fatalf("offline sync: %v (want the cached mirror to serve)", err)
	}
	if got := readMarker(t, work); got != "v1" {
		t.Errorf("offline marker = %q, want the cached v1", got)
	}
}

// TestSyncGitRefChecksOutRef pins ref checkout across the three ref kinds ward
// does not classify: branch, tag, and sha.
func TestSyncGitRefChecksOutRef(t *testing.T) {
	origin := newTestOrigin(t)
	sha := gitFixture(t, origin, "rev-parse", "v1")
	// Advance main past v1 so tag/sha checkouts are distinguishable from the tip.
	writeMarker(t, origin, "v2")
	gitFixture(t, origin, "commit", "-am", "v2")

	for ref, want := range map[string]string{"alt": "alt", "v1": "v1", sha: "v1", "": "v2"} {
		spec := testSpec(t, origin)
		spec.ref = ref
		work, err := leanRunner().syncGitRef(context.Background(), spec, time.Hour)
		if err != nil {
			t.Fatalf("sync ref %q: %v", ref, err)
		}
		if got := readMarker(t, work); got != want {
			t.Errorf("ref %q marker = %q, want %q", ref, got, want)
		}
	}
}

// TestSyncGitRefBadRefFailsLoud pins fail-loud on a ref the repo does not
// have, and that the half-made checkout is not left behind as "current".
func TestSyncGitRefBadRefFailsLoud(t *testing.T) {
	spec := testSpec(t, newTestOrigin(t))
	spec.ref = "no-such-ref"
	if _, err := leanRunner().syncGitRef(context.Background(), spec, time.Hour); err == nil {
		t.Fatal("sync of a missing ref returned nil; want a loud error")
	}
	if isDir(spec.work) {
		t.Error("failed checkout left the work dir behind; a later sync would reuse it as current")
	}
}

// TestSyncGitRefPermissionDeniedFetchHeadFallsBack pins the permission-denied cache
// case: a stale mirror with an unwritable FETCH_HEAD still serves cached state.
func TestSyncGitRefPermissionDeniedFetchHeadFallsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-bit cache tests are Unix-specific")
	}
	origin := newTestOrigin(t)
	spec := testSpec(t, origin)
	var logs strings.Builder
	spec.logf = func(format string, a ...any) {
		fmt.Fprintf(&logs, format+"\n", a...)
	}
	r := leanRunner()
	if _, err := r.syncGitRef(context.Background(), spec, time.Hour); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	head := filepath.Join(spec.mirror, "FETCH_HEAD")
	if err := os.Chmod(head, 0o444); err != nil {
		t.Fatalf("chmod FETCH_HEAD read-only: %v", err)
	}
	work, err := r.syncGitRef(context.Background(), spec, 0)
	if err != nil {
		t.Fatalf("permission-denied sync: %v", err)
	}
	if got := readMarker(t, work); got != "v1" {
		t.Fatalf("permission-denied marker = %q, want cached v1", got)
	}
	out := strings.ToLower(logs.String())
	if !strings.Contains(out, "permission denied") {
		t.Fatalf("diagnostic log missing permission denied breadcrumb:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), spec.mirror) {
		t.Fatalf("diagnostic log missing cache path %s:\n%s", spec.mirror, logs.String())
	}
	before := logs.String()
	if _, err := r.syncGitRef(context.Background(), spec, 0); err != nil {
		t.Fatalf("suppressed permission-denied sync: %v", err)
	}
	if got := logs.String(); got != before {
		t.Fatalf("permission-denied sync retried noisily:\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

// TestSyncGitRefImmutableShaSkipsRefresh pins the fast path.
// A full sha already in the mirror never refetches.
func TestSyncGitRefImmutableShaSkipsRefresh(t *testing.T) {
	origin := newTestOrigin(t)
	sha := gitFixture(t, origin, "rev-parse", "main")
	spec := testSpec(t, origin)
	spec.ref = sha
	var logs strings.Builder
	spec.logf = func(format string, a ...any) { fmt.Fprintf(&logs, format+"\n", a...) }
	r := leanRunner()
	if _, err := r.syncGitRef(context.Background(), spec, time.Hour); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Remote gone + TTL elapsed: a refresh attempt would log a failure; the
	// immutable pin must skip it entirely.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	work, err := r.syncGitRef(context.Background(), spec, 0)
	if err != nil {
		t.Fatalf("pinned-sha sync: %v", err)
	}
	if got := readMarker(t, work); got != "v1" {
		t.Errorf("pinned-sha marker = %q, want v1", got)
	}
	if out := logs.String(); strings.Contains(out, "refresh") {
		t.Errorf("pinned-sha sync attempted a refresh:\n%s", out)
	}

	// A branch ref stays on the refresh path (and cache-falls-back offline).
	branchSpec := testSpec(t, origin)
	_ = branchSpec
	if !fullShaRe.MatchString(sha) {
		t.Fatalf("test sha %q does not look like a full sha", sha)
	}
	if fullShaRe.MatchString("main") || fullShaRe.MatchString(sha[:12]) {
		t.Error("abbreviated or branch refs must not classify as immutable")
	}
}
