package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func captureTestStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	_ = w.Close()
	os.Stderr = old
	return <-done
}

func TestAgentReservationFilename(t *testing.T) {
	cases := []struct {
		ref  agentIssueRef
		want string
	}{
		{agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 142}, "coilyco-flight-deck-ward-issue-142.json"},
		{agentIssueRef{Owner: "Coily.Co", Repo: "My_Repo", Number: 7}, "coily-co-my-repo-issue-7.json"},
	}
	for _, c := range cases {
		if got := agentReservationFilename(c.ref); got != c.want {
			t.Errorf("agentReservationFilename(%s) = %q, want %q", c.ref, got, c.want)
		}
	}
}

func TestReservationFresh(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	ttl := 2 * time.Hour
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"just now", now, true},
		{"within ttl", now.Add(-time.Hour), true},
		{"exactly ttl", now.Add(-ttl), false},
		{"past ttl", now.Add(-3 * time.Hour), false},
		{"zero stamp", time.Time{}, false},
		{"future skew", now.Add(time.Minute), true},
	}
	for _, c := range cases {
		if got := reservationFresh(c.at, now, ttl); got != c.want {
			t.Errorf("%s: reservationFresh = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAgentReservationRoundTrip writes a sentinel and reads it back, then
// confirms a removed sentinel reads as absent.
func TestAgentReservationRoundTrip(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 142}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	want := agentReservation{
		Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number,
		Mode: "claude", Container: "engineer-claude-ward-142",
		Branch: "issue-142", Host: "box", PID: 4242,
		At: time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
	}
	if err := writeAgentReservation(path, want); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
	got, ok, err := readAgentReservation(path)
	if err != nil || !ok {
		t.Fatalf("readAgentReservation: ok=%v err=%v", ok, err)
	}
	if got.Container != want.Container || got.Number != want.Number || !got.At.Equal(want.At) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", *got, want)
	}
	if err := removeAgentReservation(path); err != nil {
		t.Fatalf("removeAgentReservation: %v", err)
	}
	if _, ok, _ := readAgentReservation(path); ok {
		t.Error("reservation still present after remove")
	}
	// Removing an already-gone sentinel is not an error.
	if err := removeAgentReservation(path); err != nil {
		t.Errorf("removeAgentReservation (absent): %v", err)
	}
}

// A corrupt sentinel must read as absent so it can't permanently wedge an issue.
func TestReadAgentReservationCorrupt(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if err := writeAgentReservation(path, agentReservation{Number: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Clobber with garbage.
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("clobber: %v", err)
	}
	if _, ok, err := readAgentReservation(path); ok || err != nil {
		t.Errorf("corrupt sentinel: ok=%v err=%v, want false/nil", ok, err)
	}
}

// acquireLocalReservation writes the cache sentinel and release removes it. The
// issue thread owns the blocking decision now, not the local file.
func TestAcquireLocalReservationWritesCache(t *testing.T) {
	setTestHome(t, t.TempDir())
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 9}
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	path, _ := agentReservationPath(ref)
	release, err := r.acquireLocalReservation(context.Background(), "lbl", modeClaude, ref, "fresh", "issue-9", now, false)
	if err != nil {
		t.Fatalf("acquireLocalReservation: %v", err)
	}
	if got, _, _ := readAgentReservation(path); got == nil || got.Container != "fresh" {
		t.Errorf("acquireLocalReservation should write the cache; got %+v", got)
	}
	release()
	if _, ok, _ := readAgentReservation(path); ok {
		t.Fatal("release should delete the sentinel")
	}
}

// precheckReservation refuses a fresh issue-thread hold and ignores stale cache.
func TestPrecheckReservation(t *testing.T) {
	setTestHome(t, t.TempDir())
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 184}
	now := time.Now().UTC()

	mk := func(body string, age time.Duration, login string) issueComment {
		c := issueComment{Body: body, CreatedAt: now.Add(-age)}
		c.User.Login = login
		return c
	}

	// A fresh remote reservation comment refuses, naming the holder.
	reserved := mk(reservationCommentBody(modeClaude, "ward-x", "box", now.Add(-time.Minute), "", nil), time.Minute, "coilyco-ops")
	w := resolvedWork{Ref: ref, Comments: []issueComment{reserved}}
	err := r.precheckReservation(context.Background(), "lbl", w, false)
	if err == nil || !strings.Contains(err.Error(), "reserved remotely") || !strings.Contains(err.Error(), "coilyco-ops") {
		t.Fatalf("precheckReservation: want a remote-reservation refusal naming the holder, got %v", err)
	}

	// override-reservation bypasses the remote refusal.
	if err := r.precheckReservation(context.Background(), "lbl", w, true); err != nil {
		t.Fatalf("precheckReservation override-reservation: want bypass, got %v", err)
	}

	// The precheck must NOT have written a local sentinel - it only reads.
	path, _ := agentReservationPath(ref)
	if _, ok, _ := readAgentReservation(path); ok {
		t.Error("precheckReservation must not take the hold (no sentinel should exist)")
	}

	// A clean thread (no marker) lets the run proceed to the pre-flight.
	clean := resolvedWork{Ref: ref, Comments: []issueComment{mk("just a normal comment", time.Minute, "someone")}}
	if err := r.precheckReservation(context.Background(), "lbl", clean, false); err != nil {
		t.Fatalf("precheckReservation on a clean thread: want nil, got %v", err)
	}

	// A stale local sentinel does not block when the issue thread is clean.
	if err := writeAgentReservation(path, agentReservation{
		Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number, At: now.Add(-3 * agentReservationTTL()),
		Container: "long-dead",
	}); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}
	if err := r.precheckReservation(context.Background(), "lbl", clean, false); err != nil {
		t.Fatalf("precheckReservation on stale cache: want nil, got %v", err)
	}
	if _, ok, _ := readAgentReservation(path); ok {
		t.Fatal("precheckReservation should clear stale cache")
	}
}

func TestPrecheckReservationLogs(t *testing.T) {
	setTestHome(t, t.TempDir())
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 184}
	w := resolvedWork{Ref: ref}
	got := captureTestStderr(t, func() {
		_ = r.precheckReservation(context.Background(), "lbl", w, true)
	})
	for _, want := range []string{
		"reservation precheck start",
		"reservation precheck skipped",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q:\n%s", want, got)
		}
	}
}

// TestAgentReservationLockSerializesConcurrentHarnesses proves the strict launch
// lock keeps two near-simultaneous harnesses from both deciding on the same issue.
func TestAgentReservationLockSerializesConcurrentHarnesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cli-guard flock is a documented no-op on non-Unix hosts")
	}
	setTestHome(t, t.TempDir())
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1034}

	locked := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- r.withAgentReservationLock(ref, func() error {
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case <-time.After(time.Second):
		t.Fatal("first harness did not acquire the reservation lock")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- r.withAgentReservationLock(ref, func() error { return nil })
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second harness acquired the reservation lock early: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first harness lock release: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second harness lock acquire: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second harness never acquired the reservation lock")
	}
}

func TestFreshReservationComment(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	ttl := 2 * time.Hour
	mk := func(body string, age time.Duration, login string) issueComment {
		c := issueComment{Body: body, CreatedAt: now.Add(-age)}
		c.User.Login = login
		return c
	}
	reserved := reservationCommentBody(modeClaude, "ward-x", "box", now.Add(-30*time.Minute), "", nil)

	// A fresh marker comment is a conflict and names its author.
	who, held := freshReservationComment([]issueComment{
		mk("just a normal comment", time.Minute, "someone"),
		mk(reserved, 30*time.Minute, "coilysiren"),
	}, now, ttl)
	if !held {
		t.Fatal("want a held reservation, got none")
	}
	if !strings.Contains(who, "coilysiren") {
		t.Errorf("conflict description should name the author; got %q", who)
	}

	// A stale marker is ignored.
	if _, held := freshReservationComment([]issueComment{mk(reserved, 3*time.Hour, "coilysiren")}, now, ttl); held {
		t.Error("a stale reservation marker must not block")
	}
	// The TTL is honored: under a tighter window the 30-min-old marker is stale.
	if _, held := freshReservationComment([]issueComment{mk(reserved, 30*time.Minute, "coilysiren")}, now, 10*time.Minute); held {
		t.Error("a marker older than a tighter TTL must not block")
	}
	// No marker, no conflict.
	if _, held := freshReservationComment([]issueComment{mk("plain", time.Minute, "x")}, now, ttl); held {
		t.Error("a non-reservation comment must not block")
	}

	release := reservationReleaseCommentBody(modeClaude, "ward-x", nil)

	// A release stamped after the reservation retracts it (ward#264): a smoke-test
	// death frees the hold instead of wedging the issue until the TTL lapses.
	if _, held := freshReservationComment([]issueComment{
		mk(reserved, 30*time.Minute, "coilyco-ops"),
		mk(release, 29*time.Minute, "coilyco-ops"),
	}, now, ttl); held {
		t.Error("a release after the reservation must free the issue")
	}

	// A release marker is a distinct substring from the reservation marker, so a
	// lone release without a reservation simply leaves the issue free, not held.
	if _, held := freshReservationComment([]issueComment{mk(release, time.Minute, "coilyco-ops")}, now, ttl); held {
		t.Error("a lone release comment must not register as a reservation")
	}

	// A reservation NEWER than the release still blocks (a fresh reserve/release
	// cycle followed by a new hold): the latest of each marker is what counts.
	if _, held := freshReservationComment([]issueComment{
		mk(release, 40*time.Minute, "coilyco-ops"),
		mk(reserved, 30*time.Minute, "coilyco-ops"),
	}, now, ttl); !held {
		t.Error("a reservation posted after the release must still block")
	}
}

// TestFreshReservationCommentTerminalOutcomeSupersedes pins ward#1149: a terminal
// WARD-OUTCOME posted at/after the reservation retracts it at check time.
func TestFreshReservationCommentTerminalOutcomeSupersedes(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	ttl := 2 * time.Hour
	mk := func(body string, age time.Duration) issueComment {
		c := issueComment{Body: body, CreatedAt: now.Add(-age)}
		c.User.Login = "coilyco-ops"
		return c
	}
	reserved := reservationCommentBody(modeClaude, "engineer-claude-ward-987", "box", now.Add(-30*time.Minute), "", nil)
	outcome := "WARD-OUTCOME: merge-ready\n\n<details><summary>details</summary>\n\nPR open, checks green\n\n</details>"

	// The terminal outcome after the reservation frees the issue immediately.
	if _, held := freshReservationComment([]issueComment{
		mk(reserved, 30*time.Minute),
		mk(outcome, time.Minute),
	}, now, ttl); held {
		t.Error("a terminal WARD-OUTCOME after the reservation must supersede it (ward#1149)")
	}
	// Every terminal status supersedes, including the pull-request workflows'.
	for _, status := range []string{"done ✅", "submitted", "blocked 🛑", "failed ❌"} {
		if _, held := freshReservationComment([]issueComment{
			mk(reserved, 30*time.Minute),
			mk("WARD-OUTCOME: "+status, time.Minute),
		}, now, ttl); held {
			t.Errorf("WARD-OUTCOME: %s after the reservation must supersede it", status)
		}
	}
	// A reservation NEWER than the outcome still blocks (a follow-up run's hold).
	if _, held := freshReservationComment([]issueComment{
		mk(outcome, 30*time.Minute),
		mk(reserved, time.Minute),
	}, now, ttl); !held {
		t.Error("a reservation posted after the outcome must still block")
	}
	// A non-terminal outcome line does not retract.
	if _, held := freshReservationComment([]issueComment{
		mk(reserved, 30*time.Minute),
		mk("WARD-OUTCOME: pondering", time.Minute),
	}, now, ttl); !held {
		t.Error("a non-terminal outcome must not retract the reservation")
	}
}

// TestReservationCommentBodyIsRoadBlock pins ward#494: the reservation comment carries
// the explicit "do not comment/edit to steer the run" road-block directive.
func TestReservationCommentBodyIsRoadBlock(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	body := reservationCommentBody(modeClaude, "engineer-claude-ward-494", "box", now, "", nil)
	if visible := visibleLinesBeforeDetails(body); visible != "WARD-WORKFLOW: reservation-held" {
		t.Fatalf("reservation visible line = %q\n%s", visible, body)
	}
	for _, want := range []string{
		"Do not comment on or edit this issue",
		"new issue, dispatched fresh",
		"ward#494",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reservation comment missing road-block phrase %q\n got: %s", want, body)
		}
	}
}

// fakeLockForge is a near-no-op Tracker recording lock/unlock and comment posts, so
// lockReservedIssue (ward#494) and releaseRemoteReservation (ward#570) can be exercised.
type fakeLockForge struct {
	lockErr      error
	locked       int
	unlockErr    error
	unlocked     int
	commentErr   error
	comments     []string
	deleted      []int
	listComments []issueComment
	listErr      error
	listCalls    int
}

func (f *fakeLockForge) GetIssue(context.Context, string, string, int) (*Issue, error) {
	return &Issue{}, nil
}
func (f *fakeLockForge) ListIssueComments(context.Context, string, string, int) ([]issueComment, error) {
	f.listCalls++
	return f.listComments, f.listErr
}
func (f *fakeLockForge) CreateIssue(context.Context, string, string, string, string) (int, error) {
	return 0, nil
}
func (f *fakeLockForge) CommentIssue(_ context.Context, _, _ string, _ int, body string) error {
	if f.commentErr != nil {
		return f.commentErr
	}
	f.comments = append(f.comments, body)
	return nil
}
func (f *fakeLockForge) DeleteIssueComment(_ context.Context, _, _ string, commentID int) error {
	f.deleted = append(f.deleted, commentID)
	return nil
}
func (f *fakeLockForge) CloseIssue(context.Context, string, string, int) error  { return nil }
func (f *fakeLockForge) ReopenIssue(context.Context, string, string, int) error { return nil }
func (f *fakeLockForge) LockIssue(context.Context, string, string, int) error {
	f.locked++
	return f.lockErr
}
func (f *fakeLockForge) UnlockIssue(context.Context, string, string, int) error {
	f.unlocked++
	return f.unlockErr
}

// TestLockReservedIssue covers ward#494's three outcomes - locked, unsupported-forge
// road-block fallback, and soft-failure warning - none of which fails the reservation.
func TestLockReservedIssue(t *testing.T) {
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 494}
	cases := []struct {
		name    string
		lockErr error
		want    string
	}{
		{"locked", nil, "locked issue"},
		{"unsupported", errForgeLockUnsupported, "left unlocked"},
		{"soft failure", errors.New("403 forbidden"), "could not lock"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeLockForge{lockErr: c.lockErr}
			got := captureTestStderr(t, func() {
				r.lockReservedIssue(context.Background(), f, "lbl", ref)
			})
			if f.locked != 1 {
				t.Fatalf("lockIssue called %d times, want 1", f.locked)
			}
			if !strings.Contains(got, c.want) {
				t.Fatalf("log missing %q:\n%s", c.want, got)
			}
		})
	}
}

// TestReleaseRemoteReservation covers ward#570's rollback: a failed launch retracts the
// forge road-block via a release comment + unlock, loud but non-blocking on failure.
func TestReleaseRemoteReservation(t *testing.T) {
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 570}

	// Happy path: one release-marker comment, then an unlock, with a confirming log.
	t.Run("posts release and unlocks", func(t *testing.T) {
		f := &fakeLockForge{
			listComments: []issueComment{
				{ID: 42, Body: reservationCommentBody(modeClaude, "engineer-claude-ward-570", "host", time.Now().Add(-time.Minute), "", nil), CreatedAt: time.Now().Add(-time.Minute)},
			},
		}
		out := captureTestStderr(t, func() {
			r.releaseRemoteReservation(context.Background(), f, "lbl", modeClaude, ref, "engineer-claude-ward-570")
		})
		if len(f.comments) != 1 || !strings.Contains(f.comments[0], agentReservationReleaseMarker) {
			t.Fatalf("want one release-marker comment, got %v", f.comments)
		}
		if f.unlocked != 1 {
			t.Fatalf("unlockIssue called %d times, want 1", f.unlocked)
		}
		if got := fmt.Sprintf("%v", f.deleted); got != "[42]" {
			t.Fatalf("deleted comments = %s, want [42]", got)
		}
		if !strings.Contains(out, "released remote reservation") {
			t.Fatalf("missing release log:\n%s", out)
		}
	})

	// A failed release post warns and skips the unlock - there is nothing to retract, and
	// the rollback must not blow up on a best-effort forge call.
	t.Run("comment failure warns and skips unlock", func(t *testing.T) {
		f := &fakeLockForge{commentErr: errors.New("500 boom")}
		out := captureTestStderr(t, func() {
			r.releaseRemoteReservation(context.Background(), f, "lbl", modeClaude, ref, "c1")
		})
		if f.unlocked != 0 {
			t.Fatalf("unlock ran after a failed release post; got %d", f.unlocked)
		}
		if !strings.Contains(out, "could not release the remote reservation") {
			t.Fatalf("missing release-failure warn:\n%s", out)
		}
	})

	// The no-lock-leaf forge (Forgejo) returns errForgeLockUnsupported on unlock; that is
	// the expected steady state, so it must not warn.
	t.Run("unsupported unlock stays quiet", func(t *testing.T) {
		f := &fakeLockForge{unlockErr: errForgeLockUnsupported}
		out := captureTestStderr(t, func() {
			r.releaseRemoteReservation(context.Background(), f, "lbl", modeClaude, ref, "c1")
		})
		if strings.Contains(out, "could not unlock") {
			t.Fatalf("unsupported unlock must stay silent:\n%s", out)
		}
		if !strings.Contains(out, "released remote reservation") {
			t.Fatalf("missing release log:\n%s", out)
		}
	})

	// A soft unlock failure (not the unsupported sentinel) warns but still logs the release.
	t.Run("soft unlock failure warns", func(t *testing.T) {
		f := &fakeLockForge{unlockErr: errors.New("403 forbidden")}
		out := captureTestStderr(t, func() {
			r.releaseRemoteReservation(context.Background(), f, "lbl", modeClaude, ref, "c1")
		})
		if !strings.Contains(out, "could not unlock") {
			t.Fatalf("soft unlock failure should warn:\n%s", out)
		}
	})
}

// TestWaitForDispatchBrokerEngineerVisibilityReleasesFailedLaunchIntent keeps the
// launch-intent cleanup immediate when a launch never becomes visible.
func TestWaitForDispatchBrokerEngineerVisibilityReleasesFailedLaunchIntent(t *testing.T) {
	setTestHome(t, t.TempDir())
	origTimeout := dispatchBrokerVisibilityTimeout
	origPoll := dispatchBrokerVisibilityPoll
	dispatchBrokerVisibilityTimeout = 25 * time.Millisecond
	dispatchBrokerVisibilityPoll = time.Millisecond
	t.Cleanup(func() {
		dispatchBrokerVisibilityTimeout = origTimeout
		dispatchBrokerVisibilityPoll = origPoll
	})

	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 991}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	now := time.Now().UTC()
	if err := writeAgentReservation(path, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeClaude),
		Container: "engineer-claude-ward-991",
		Branch:    "issue-991",
		Host:      "director-box",
		At:        now,
	}); err != nil {
		t.Fatalf("writeAgentReservation: %v", err)
	}
	called := false
	registerDispatchLaunchReservationRelease(ref, func() {
		called = true
		_ = removeAgentReservation(path)
	})
	r, _, _ := bufRunner(dockerAbsentStub(t))
	err = r.waitForDispatchBrokerEngineerVisibility(context.Background(), dispatchBrokerRequest{Argv: []string{"engineer", ref.String()}})
	if err == nil {
		t.Fatal("waitForDispatchBrokerEngineerVisibility: want a visibility failure, got nil")
	}
	if !called {
		t.Fatal("visibility failure should immediately release the pending launch intent")
	}
	if _, ok, _ := readAgentReservation(path); ok {
		t.Fatal("visibility failure should remove the local launch-intent sentinel")
	}
}

// TestForgejoLockUnsupported asserts the Forgejo client reports no lock leaf, the
// signal lockReservedIssue downgrades to the comment road-block (ward#494).
func TestForgejoLockUnsupported(t *testing.T) {
	c := &forgejoClient{}
	if err := c.LockIssue(context.Background(), "o", "r", 1); !errors.Is(err, errForgeLockUnsupported) {
		t.Errorf("forgejo lockIssue = %v, want errForgeLockUnsupported", err)
	}
	if err := c.UnlockIssue(context.Background(), "o", "r", 1); !errors.Is(err, errForgeLockUnsupported) {
		t.Errorf("forgejo unlockIssue = %v, want errForgeLockUnsupported", err)
	}
}

// TestIsReservationConflict covers ward#352's classifier: a typed conflict (even wrapped)
// reads as a conflict; a plain launch error and nil do not.
func TestIsReservationConflict(t *testing.T) {
	conflict := newReservationConflict("issue %s is already reserved remotely", "a/b#5")
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"typed conflict", conflict, true},
		{"wrapped conflict", fmt.Errorf("dispatch: %w", conflict), true},
		{"plain launch error", errors.New("image pull failed"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isReservationConflict(c.err); got != c.want {
				t.Errorf("isReservationConflict(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func dockerRunningStub(t *testing.T, name string) string {
	t.Helper()
	stub := t.TempDir() + "/docker"
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = ps ] && [ \"$2\" = --filter ] && [ \"$3\" = " + shellQuote("name=^"+name+"$") + " ] && [ \"$4\" = --format ]; then\n" +
		"  printf '%s\\n' " + shellQuote(name) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, stub, script)
	return stub
}

func dockerAbsentStub(t *testing.T) string {
	t.Helper()
	stub := t.TempDir() + "/docker"
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = ps ]; then\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, stub, script)
	return stub
}

func TestAcquireLocalReservationDoesNotBlockOnRunningWorkerCache(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 788}
	container := "engineer-claude-ward-788"
	r, _, _ := bufRunner(dockerRunningStub(t, container))
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("sentinel unexpectedly existed before the acquire: %v", err)
	}
	release, err := r.acquireLocalReservation(context.Background(), "lbl", modeClaude, ref, container, "issue-788", time.Now().UTC(), false)
	if err != nil {
		t.Fatalf("acquireLocalReservation: %v", err)
	}
	if got, ok, _ := readAgentReservation(path); !ok || got.Container != container {
		t.Fatalf("acquireLocalReservation should write the cache, got %+v ok=%v", got, ok)
	}
	release()
}

func TestAcquireLocalReservationReleaseLeavesForeignSentinel(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 789}
	container := "engineer-claude-ward-789"
	r, _, _ := bufRunner(dockerAbsentStub(t))
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("agentReservationPath: %v", err)
	}
	now := time.Date(2026, 7, 9, 9, 0, 0, 0, time.UTC)
	release, err := r.acquireLocalReservation(context.Background(), "lbl", modeClaude, ref, container, "issue-789", now, false)
	if err != nil {
		t.Fatalf("acquireLocalReservation: %v", err)
	}
	foreign := agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeClaude),
		Container: "engineer-claude-ward-789-other",
		Branch:    "issue-789-other",
		Host:      "other-host",
		PID:       4242,
		At:        now.Add(time.Minute),
	}
	if err := writeAgentReservation(path, foreign); err != nil {
		t.Fatalf("overwrite reservation with foreign owner: %v", err)
	}
	release()
	got, ok, err := readAgentReservation(path)
	if err != nil {
		t.Fatalf("readAgentReservation after release: %v", err)
	}
	if !ok {
		t.Fatal("release deleted a sentinel that no longer belonged to this launch")
	}
	if got == nil || !reflect.DeepEqual(*got, foreign) {
		t.Fatalf("reservation after release = %+v, want %+v", got, foreign)
	}
}

// TestPostReservationComment covers ward#402's bounded retry: a clean first post, a
// ride over transient failures, and a give-up after the attempts run out.
func TestPostReservationComment(t *testing.T) {
	transient := errors.New("rate limited")

	// Succeeds first try: one attempt, no sleeps.
	t.Run("first try", func(t *testing.T) {
		sleeps := 0
		calls := 0
		tries, err := postReservationComment(context.Background(), 3, time.Second,
			func(time.Duration) { sleeps++ },
			func(context.Context) error { calls++; return nil })
		if err != nil || tries != 1 || calls != 1 || sleeps != 0 {
			t.Fatalf("first-try: tries=%d calls=%d sleeps=%d err=%v", tries, calls, sleeps, err)
		}
	})

	// Fails twice then lands: three calls, two backoff sleeps, nil error.
	t.Run("retry then succeed", func(t *testing.T) {
		sleeps := 0
		calls := 0
		tries, err := postReservationComment(context.Background(), 3, time.Second,
			func(time.Duration) { sleeps++ },
			func(context.Context) error {
				calls++
				if calls < 3 {
					return transient
				}
				return nil
			})
		if err != nil || tries != 3 || calls != 3 || sleeps != 2 {
			t.Fatalf("retry-then-succeed: tries=%d calls=%d sleeps=%d err=%v", tries, calls, sleeps, err)
		}
	})

	// Always fails: exhausts attempts, returns the last error, sleeps attempts-1 times
	// (never after the final try).
	t.Run("exhausts", func(t *testing.T) {
		sleeps := 0
		calls := 0
		tries, err := postReservationComment(context.Background(), 3, time.Second,
			func(time.Duration) { sleeps++ },
			func(context.Context) error { calls++; return transient })
		if !errors.Is(err, transient) || tries != 3 || calls != 3 || sleeps != 2 {
			t.Fatalf("exhausts: tries=%d calls=%d sleeps=%d err=%v", tries, calls, sleeps, err)
		}
	})

	// A non-positive attempt count is clamped to one real try (no sleep).
	t.Run("clamps attempts", func(t *testing.T) {
		calls := 0
		tries, err := postReservationComment(context.Background(), 0, time.Second,
			func(time.Duration) { t.Fatal("must not sleep on a single attempt") },
			func(context.Context) error { calls++; return transient })
		if !errors.Is(err, transient) || tries != 1 || calls != 1 {
			t.Fatalf("clamps: tries=%d calls=%d err=%v", tries, calls, err)
		}
	})
}

// TestReserveIssueReportsPartialLaunchWhenReservationCommentPostFails keeps the
// reservation-post warning actionable once the launch continues past it.
func TestReserveIssueReportsPartialLaunchWhenReservationCommentPostFails(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv(reservationRecheckEnv, "0")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues/1360/comments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`[]`))
		case http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`reservation post failed`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	origForgejoBase := forgejoBaseURL
	forgejoBaseURL = srv.URL
	t.Cleanup(func() { forgejoBaseURL = origForgejoBase })

	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1360}
	release, partial, err := r.reserveIssue(context.Background(), "ward agent engineer", modeCodex, ref, "engineer-codex-ward-1360", "issue-1360", "", nil, false, false)
	if err != nil {
		t.Fatalf("reserveIssue: %v", err)
	}
	if release == nil {
		t.Fatal("reserveIssue returned a nil release func")
	}
	if partial == nil {
		t.Fatal("reserveIssue did not report a partial-launch notice")
	}
	if !isPartialLaunchError(partial) {
		t.Fatalf("partial notice = %T %v, want a partial-launch error", partial, partial)
	}
	for _, want := range []string{
		ref.String(),
		"engineer-codex-ward-1360",
		"reservation-held marker",
		"re-post the reservation comment or stop and re-dispatch engineer-codex-ward-1360",
	} {
		if !strings.Contains(partial.Error(), want) {
			t.Fatalf("partial-launch notice missing %q:\n%s", want, partial.Error())
		}
	}
}

func TestReservationCommentBodyHasMarker(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	body := reservationCommentBody(modeCodex, "engineer-codex-ward-142", "tower", now, "", nil)
	for _, want := range []string{agentReservationMarker, "WARD-WORKFLOW: reservation-held", "ward agent --harness codex", "engineer-codex-ward-142", "tower", "3h TTL"} {
		if !strings.Contains(body, want) {
			t.Errorf("reservation comment missing %q\n got: %s", want, body)
		}
	}
	// With no justification the comment stays bare - no empty GO block (ward#383).
	if strings.Contains(body, "GO") {
		t.Errorf("reservation comment folded in a GO block with no justification\n got: %s", body)
	}
}

// TestReservationCommentBodyFoldsJustification pins ward#383: a GO pre-flight read
// is folded into the reservation comment, verbatim, inside a collapsed block.
func TestReservationCommentBodyFoldsJustification(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	read := "Main risk is the schema migration.\n\nGO"
	body := reservationCommentBody(modeClaude, "engineer-claude-ward-383", "box", now, "  "+read+"  ", nil)
	for _, want := range []string{"reservation details", "pre-flight read (GO)", "<details>", read, "**GO**"} {
		if !strings.Contains(body, want) {
			t.Errorf("reservation comment missing %q\n got: %s", want, body)
		}
	}
	// The marker still leads so freshReservationComment recognizes it as a hold.
	if !strings.HasPrefix(body, agentReservationMarker) {
		t.Errorf("reservation comment lost its leading marker\n got: %s", body)
	}
}

// TestWinningReservationClaim covers the deterministic tiebreak (ward#600): earliest
// timestamp wins, ties break on the lexically-min identity, and an empty set has none.
func TestWinningReservationClaim(t *testing.T) {
	t0 := time.Date(2026, 7, 4, 8, 28, 20, 0, time.UTC)
	if _, ok := winningReservationClaim(nil); ok {
		t.Errorf("winningReservationClaim(nil) reported a winner")
	}
	// Earliest timestamp wins regardless of input order.
	claims := []reservationClaim{
		{at: t0.Add(2 * time.Second), identity: "aaa@h"},
		{at: t0, identity: "zzz@h"},
		{at: t0.Add(time.Second), identity: "mmm@h"},
	}
	if w, _ := winningReservationClaim(claims); w.identity != "zzz@h" {
		t.Errorf("earliest-timestamp winner = %q, want zzz@h", w.identity)
	}
	// Same timestamp -> lexically-min identity wins.
	tie := []reservationClaim{
		{at: t0, identity: "engineer@tower"},
		{at: t0, identity: "engineer@desktop"},
	}
	if w, _ := winningReservationClaim(tie); w.identity != "engineer@desktop" {
		t.Errorf("tie winner = %q, want engineer@desktop", w.identity)
	}
}

// TestReservationClaims filters the thread down to live, un-released reservation claims
// and parses each identity from the marker body (ward#600).
func TestReservationClaims(t *testing.T) {
	now := time.Now().UTC()
	ttl := agentReservationTTL()
	a := issueComment{Body: reservationCommentBody(modeClaude, "engineer-claude-ward-600", "host-a", now.Add(-time.Minute), "", nil), CreatedAt: now.Add(-time.Minute)}
	b := issueComment{Body: reservationCommentBody(modeClaude, "engineer-claude-ward-600", "host-b", now.Add(-30*time.Second), "", nil), CreatedAt: now.Add(-30 * time.Second)}
	stale := issueComment{Body: reservationCommentBody(modeClaude, "engineer-claude-ward-600", "host-c", now.Add(-3*time.Hour), "", nil), CreatedAt: now.Add(-3 * time.Hour)}
	chat := issueComment{Body: "just a human comment", CreatedAt: now}
	claims := reservationClaims([]issueComment{a, b, stale, chat}, now, ttl)
	if len(claims) != 2 {
		t.Fatalf("reservationClaims returned %d, want 2 (fresh a+b, stale + chat dropped)", len(claims))
	}
	got := map[string]bool{claims[0].identity: true, claims[1].identity: true}
	if !got["engineer-claude-ward-600@host-a"] || !got["engineer-claude-ward-600@host-b"] {
		t.Errorf("reservationClaims identities = %v, want the host-a/host-b pair", got)
	}
	// A release stamped at/after the latest claim retracts every claim.
	rel := issueComment{Body: reservationReleaseCommentBody(modeClaude, "engineer-claude-ward-600", nil), CreatedAt: now}
	if c := reservationClaims([]issueComment{a, b, rel}, now, ttl); len(c) != 0 {
		t.Errorf("a release at/after the claims left %d live, want 0", len(c))
	}
	// A terminal outcome at/after the claims retracts them like a release (ward#1149).
	oc := issueComment{Body: "WARD-OUTCOME: merge-ready", CreatedAt: now}
	if c := reservationClaims([]issueComment{a, b, oc}, now, ttl); len(c) != 0 {
		t.Errorf("a terminal outcome at/after the claims left %d live, want 0", len(c))
	}
}

// TestReservationRecheckLost drives the double-check: an earlier rival makes this run
// yield; its own/sole claim, a disabled window, or a read error all proceed (ward#600).
func TestReservationRecheckLost(t *testing.T) {
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 600}
	now := time.Now().UTC()
	const container = "engineer-claude-ward-600"

	origDelay, origSleep := reservationRecheckDelay, reservationRecheckSleep
	reservationRecheckSleep = func(time.Duration) {}
	defer func() { reservationRecheckDelay, reservationRecheckSleep = origDelay, origSleep }()

	ourComment := issueComment{Body: reservationCommentBody(modeClaude, container, hostname(), now, "", nil), CreatedAt: now}
	rival := issueComment{Body: reservationCommentBody(modeClaude, container, "rival-host", now.Add(-2*time.Second), "", nil), CreatedAt: now.Add(-2 * time.Second)}

	t.Run("earlier rival wins -> we yield", func(t *testing.T) {
		reservationRecheckDelay = func() time.Duration { return time.Millisecond }
		f := &fakeLockForge{listComments: []issueComment{ourComment, rival}}
		lost, winner := r.reservationRecheckLost(context.Background(), f, "label", ref, container, false)
		if !lost {
			t.Fatalf("expected to yield to the earlier rival, got lost=false")
		}
		if winner != container+"@rival-host" {
			t.Errorf("winner = %q, want %s@rival-host", winner, container)
		}
	})

	t.Run("we are earliest -> proceed", func(t *testing.T) {
		reservationRecheckDelay = func() time.Duration { return time.Millisecond }
		later := issueComment{Body: reservationCommentBody(modeClaude, container, "rival-host", now.Add(2*time.Second), "", nil), CreatedAt: now.Add(2 * time.Second)}
		f := &fakeLockForge{listComments: []issueComment{ourComment, later}}
		if lost, _ := r.reservationRecheckLost(context.Background(), f, "label", ref, container, false); lost {
			t.Errorf("earliest claim should proceed, got lost=true")
		}
	})

	t.Run("disabled window -> proceed without reading", func(t *testing.T) {
		reservationRecheckDelay = func() time.Duration { return 0 }
		f := &fakeLockForge{listComments: []issueComment{rival}}
		if lost, _ := r.reservationRecheckLost(context.Background(), f, "label", ref, container, false); lost {
			t.Errorf("disabled re-check should never yield, got lost=true")
		}
	})

	t.Run("read error -> fail open", func(t *testing.T) {
		reservationRecheckDelay = func() time.Duration { return time.Millisecond }
		f := &fakeLockForge{listErr: errors.New("forge down")}
		if lost, _ := r.reservationRecheckLost(context.Background(), f, "label", ref, container, false); lost {
			t.Errorf("a read failure must fail open, got lost=true")
		}
	})

	t.Run("skip-preflight -> skip wait and reread", func(t *testing.T) {
		reservationRecheckDelay = func() time.Duration {
			t.Fatal("reservationRecheckDelay must not run when --skip-preflight is set")
			return 0
		}
		f := &fakeLockForge{listComments: []issueComment{rival}}
		got := captureTestStderr(t, func() {
			lost, winner := r.reservationRecheckLost(context.Background(), f, "label", ref, container, true)
			if lost || winner != "" {
				t.Fatalf("skip-preflight recheck = lost=%v winner=%q, want false/empty", lost, winner)
			}
		})
		if f.listCalls != 0 {
			t.Fatalf("skip-preflight recheck reread thread %d times, want 0", f.listCalls)
		}
		if !strings.Contains(got, "skipping reservation re-check (--skip-preflight)") {
			t.Fatalf("skip-preflight recheck log missing skip line:\n%s", got)
		}
	})
}

// TestReservationRecheckMax covers the env override parse: default, explicit duration,
// the disable tokens, and an unparseable value falling back to the default (ward#600).
func TestReservationRecheckMax(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", reservationRecheckDefaultMax()},
		{"30s", 30 * time.Second},
		{"0", 0},
		{"off", 0},
		{"none", 0},
		{"garbage", reservationRecheckDefaultMax()},
		{"-5s", reservationRecheckDefaultMax()},
	}
	for _, c := range cases {
		t.Setenv(reservationRecheckEnv, c.env)
		if c.env == "" {
			os.Unsetenv(reservationRecheckEnv)
		}
		if got := reservationRecheckMax(); got != c.want {
			t.Errorf("reservationRecheckMax(env=%q) = %s, want %s", c.env, got, c.want)
		}
	}
}

// --- ward#609: reservation seed context + gate-failure release enrichment -----

// TestReservationSeedContextRender pins the folded seed context (ward#609): a collapsed
// <details> of dynamic per-run bytes (refs, run shape, thread tally, comment identities).
func TestReservationSeedContextRender(t *testing.T) {
	now := time.Date(2026, 7, 5, 2, 13, 22, 0, time.UTC)
	sc := &reservationSeedContext{
		Ref:          agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 609},
		Branch:       "issue-609",
		Driver:       "claude",
		RunID:        "engineer-claude-ward-609",
		WardVersion:  "v0.80.0",
		Workflow:     workflowDirectToMain,
		Reservation:  "held",
		Included:     []reservationThreadEntry{{Author: "kai", At: now.Add(-time.Hour)}},
		Stripped:     []reservationThreadEntry{{Author: "coilyco-ops", At: now.Add(-30 * time.Minute)}},
		DispatchedAt: now,
	}
	got := sc.render()
	for _, want := range []string{
		"<details><summary>run seed context",
		"coilyco-flight-deck/ward#609",
		"branch `issue-609`",
		"harness `claude`",
		"engineer-claude-ward-609",
		"v0.80.0",
		"2026-07-05T02:13:22Z",
		"**Reservation:** held",
		"1 included in the pre-flight read, 1 stripped",
		"@kai",
		"@coilyco-ops",
		"Static container doctrine and seed boilerplate are identical every run and omitted",
		"</details>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q\n got: %s", want, got)
		}
	}
	for _, notWant := range []string{"Issue body as seeded:", "Something broke."} {
		if strings.Contains(got, notWant) {
			t.Errorf("render should omit %q\n got: %s", notWant, got)
		}
	}
	// A nil context renders nothing (an override path with no captured context).
	if s := (*reservationSeedContext)(nil).render(); s != "" {
		t.Errorf("nil render should be empty, got %q", s)
	}
}

// TestLaunchPreflightSeedContext carries the pre-flight transcript into the
// launched seed artifact instead of the host audit stream.
func TestLaunchPreflightSeedContext(t *testing.T) {
	now := time.Date(2026, 7, 5, 2, 13, 22, 0, time.UTC)
	sc := &reservationSeedContext{
		Ref:          agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1335},
		Branch:       "issue-1335",
		Driver:       "codex",
		RunID:        "engineer-codex-ward-1335",
		WardVersion:  "v0.81.0",
		Workflow:     workflowPullRequestAndMerge,
		Reservation:  "held",
		Included:     []reservationThreadEntry{{Author: "kai", At: now}},
		DispatchedAt: now,
	}
	t.Run("normal launch defers the re-check result", func(t *testing.T) {
		got := launchPreflightSeedContext(sc, preflightOutcome{Verdict: verdictGo}, "GO\n\ncarry it")
		for _, want := range []string{
			"----- launch pre-flight -----",
			"host pre-flight: passed",
			"checked: trusted owner and issue/ref resolution -> passed",
			"reservation re-check: deferred",
			"reservation state: held",
			"resolved launch context:",
			"coilyco-flight-deck/ward#1335",
			"branch `issue-1335`",
			"workflow `pull-request-and-merge`",
			"pre-flight read:",
			"GO",
			"carry it",
			"----- end launch pre-flight -----",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("launch pre-flight seed missing %q\n got: %s", want, got)
			}
		}
		if strings.Contains(got, "reservation re-check: passed") {
			t.Fatalf("launch pre-flight seed should not claim a future re-check result:\n%s", got)
		}
		if strings.Contains(got, "host dispatch broker") {
			t.Fatalf("launch pre-flight seed should not contain broker-only logs:\n%s", got)
		}
	})

	t.Run("skip-preflight still defers the re-check result", func(t *testing.T) {
		got := launchPreflightSeedContext(sc, preflightOutcome{Verdict: verdictUnknown}, "skipping reservation re-check (--skip-preflight)")
		if !strings.Contains(got, "reservation re-check: deferred") {
			t.Fatalf("skip-preflight launch seed missing deferred re-check status:\n%s", got)
		}
		if strings.Contains(got, "reservation re-check: passed") {
			t.Fatalf("skip-preflight launch seed must not claim the re-check passed:\n%s", got)
		}
	})
}

// TestBuildReservationSeedContextPartition pins the included-vs-stripped split: it
// mirrors the pre-flight strip predicate exactly (ward#609).
func TestBuildReservationSeedContextPartition(t *testing.T) {
	now := time.Date(2026, 7, 5, 2, 13, 0, 0, time.UTC)
	mk := func(body, login string) issueComment {
		c := issueComment{Body: body, CreatedAt: now}
		c.User.Login = login
		return c
	}
	w := resolvedWork{
		Ref:      agentIssueRef{Owner: "o", Repo: "r", Number: 1},
		Body:     "body",
		Workflow: workflowDirectToMain,
		Comments: []issueComment{
			mk("a real human comment", "kai"),                                             // included
			mk(reservationCommentBody(modeClaude, "c", "h", now, "", nil), "coilyco-ops"), // stripped (reservation marker)
			mk("", "ghost"), // stripped (empty)
			mk(preflightNoGoMarker+"\nNO-GO: nope", "coilyco-ops"), // stripped (nogo marker)
		},
	}
	plan := upPlan{Name: "engineer-claude-o-1", Branch: "issue-1", Mode: modeClaude, WardVersion: "dev"}
	sc := buildReservationSeedContext(w, plan, now)
	if len(sc.Included) != 1 || sc.Included[0].Author != "kai" {
		t.Fatalf("included: want [kai], got %+v", sc.Included)
	}
	if len(sc.Stripped) != 3 {
		t.Fatalf("stripped: want 3, got %d (%+v)", len(sc.Stripped), sc.Stripped)
	}
	// A dev pin renders as the resolve-in-container label.
	if got := reservationWardVersionLabel(sc.WardVersion); !strings.Contains(got, "latest") {
		t.Errorf("dev ward version label: got %q", got)
	}
}

// TestReservationReleaseCommentBodyGate pins the enriched release comment (ward#609):
// a nil gate keeps the generic text; a gate names the gate, recovery, and error.
func TestReservationReleaseCommentBodyGate(t *testing.T) {
	generic := reservationReleaseCommentBody(modeClaude, "engineer-claude-ward-609", nil)
	if !strings.HasPrefix(generic, agentReservationReleaseMarker) || !strings.Contains(generic, "smoke-test death") {
		t.Fatalf("generic release comment regressed: %s", generic)
	}
	if visible := visibleLinesBeforeDetails(generic); visible != "WARD-WORKFLOW: reservation-released" {
		t.Fatalf("generic release visible line = %q\n%s", visible, generic)
	}
	if strings.Contains(generic, "**Gate:**") {
		t.Errorf("generic release comment should carry no Gate section: %s", generic)
	}
	// ward#595: a pre-launch death is loud + machine-detectable, not a benign release.
	for _, want := range []string{agentNeedsRedispatchMarker, "Run never started", "needs re-dispatch"} {
		if !strings.Contains(generic, want) {
			t.Errorf("generic release comment missing %q\n got: %s", want, generic)
		}
	}
	// auth gate -> names the gate, the recovery, and the folded error line.
	gf := &gateFailure{Gate: "auth", Detail: "auth smoke test: claude -p rejected the credentials (exit 1)"}
	enriched := reservationReleaseCommentBody(modeClaude, "engineer-claude-ward-609", gf)
	if visible := visibleLinesBeforeDetails(enriched); visible != "WARD-WORKFLOW: reservation-released" {
		t.Fatalf("enriched release visible line = %q\n%s", visible, enriched)
	}
	for _, want := range []string{
		agentReservationReleaseMarker,
		agentNeedsRedispatchMarker,
		"Run never started",
		"release details",
		"**auth** pre-launch gate",
		"**Gate:** auth smoke test (claude credentials)",
		"**Recovery:** Refresh the host claude login",
		"Error from the gate",
		"rejected the credentials",
	} {
		if !strings.Contains(enriched, want) {
			t.Errorf("enriched release comment missing %q\n got: %s", want, enriched)
		}
	}
}

// TestGateRecovery covers the known gates plus the unknown fallthrough.
func TestGateRecovery(t *testing.T) {
	for _, c := range []struct{ gate, wantLabel, wantRecov string }{
		{"auth", "auth smoke test", "Refresh the host claude login"},
		{"ollama-probe", "ollama reachability probe", "Ollama endpoint up"},
		{"codex-probe", "codex launch probe", "codex config/auth"},
		{"model-config", "model-config pre-launch gate", "Update the model environment"},
		{"bootstrap", "container bootstrap", "failing bootstrap step"},
		{"mystery", "mystery", "docker logs"},
	} {
		label, recovery := gateRecovery(c.gate)
		if !strings.Contains(label, c.wantLabel) || !strings.Contains(recovery, c.wantRecov) {
			t.Errorf("gateRecovery(%q) = (%q,%q), want labels ~%q/%q", c.gate, label, recovery, c.wantLabel, c.wantRecov)
		}
	}
}

// TestParseGateFailure covers the two-part record, the missing-gate reject, and blanks.
func TestParseGateFailure(t *testing.T) {
	if gf := parseGateFailure("gate=auth\nthe error line\nmore detail"); gf == nil ||
		gf.Gate != "auth" || gf.Detail != "the error line\nmore detail" {
		t.Fatalf("parse: got %+v", gf)
	}
	if gf := parseGateFailure("gate=ollama-probe\n"); gf == nil || gf.Gate != "ollama-probe" || gf.Detail != "" {
		t.Fatalf("parse gate-only: got %+v", gf)
	}
	for _, bad := range []string{"", "   ", "no gate here", "gate=\ndetail"} {
		if gf := parseGateFailure(bad); gf != nil {
			t.Errorf("parseGateFailure(%q) should be nil, got %+v", bad, gf)
		}
	}
}

// TestReadGateFailure round-trips through the file the entrypoint writes.
func TestReadGateFailure(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/gate-failure"
	t.Setenv(gateFailureEnv, path)
	// Absent file -> (nil, false).
	if gf, ok := readGateFailure(); ok || gf != nil {
		t.Fatalf("absent file: want (nil,false), got (%+v,%v)", gf, ok)
	}
	if err := os.WriteFile(path, []byte("gate=bootstrap\nward did not install correctly\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gf, ok := readGateFailure()
	if !ok || gf == nil || gf.Gate != "bootstrap" || !strings.Contains(gf.Detail, "install correctly") {
		t.Fatalf("read: got (%+v,%v)", gf, ok)
	}
}
