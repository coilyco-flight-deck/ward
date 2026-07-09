package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
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
	t.Setenv("HOME", t.TempDir())
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
	t.Setenv("HOME", t.TempDir())
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

// acquireLocalReservation must refuse a fresh hold (whose container we can't
// disprove is running) and then take it over once --force is set.
func TestAcquireLocalReservationConflictAndForce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 142}
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	path, _ := agentReservationPath(ref)

	// A fresh prior hold with an empty container name (liveness unknown -> treated
	// as still held) must block.
	if err := writeAgentReservation(path, agentReservation{
		Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number, At: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed prior hold: %v", err)
	}
	if _, err := r.acquireLocalReservation(context.Background(), "lbl", modeClaude, ref, "c1", "issue-142", now, false); err == nil {
		t.Fatal("acquireLocalReservation: want conflict error on a fresh hold, got nil")
	}
	// --force reclaims it.
	release, err := r.acquireLocalReservation(context.Background(), "lbl", modeClaude, ref, "c1", "issue-142", now, true)
	if err != nil {
		t.Fatalf("acquireLocalReservation --force: %v", err)
	}
	if got, ok, _ := readAgentReservation(path); !ok || got.Container != "c1" {
		t.Errorf("after force the sentinel should be ours; got %+v ok=%v", got, ok)
	}
	release()
	if _, ok, _ := readAgentReservation(path); ok {
		t.Error("release should delete the sentinel")
	}
}

// A stale prior hold (older than the TTL) is reclaimed without --force and
// without consulting docker liveness.
func TestAcquireLocalReservationStaleReclaim(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 9}
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	path, _ := agentReservationPath(ref)
	if err := writeAgentReservation(path, agentReservation{
		Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number,
		Container: "long-dead", At: now.Add(-3 * agentReservationTTL()),
	}); err != nil {
		t.Fatalf("seed stale hold: %v", err)
	}
	release, err := r.acquireLocalReservation(context.Background(), "lbl", modeClaude, ref, "fresh", "issue-9", now, false)
	if err != nil {
		t.Fatalf("acquireLocalReservation over stale: %v", err)
	}
	if got, _, _ := readAgentReservation(path); got == nil || got.Container != "fresh" {
		t.Errorf("stale hold should be reclaimed; got %+v", got)
	}
	release()
}

// precheckReservation refuses a fresh remote/local hold without writing a sentinel
// (so the hold stays untaken) and --force bypasses both (ward#184 ordering).
func TestPrecheckReservation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

	// --force bypasses the remote refusal.
	if err := r.precheckReservation(context.Background(), "lbl", w, true); err != nil {
		t.Fatalf("precheckReservation --force: want bypass, got %v", err)
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

	// A fresh local sentinel (empty container -> liveness unknown -> held) refuses
	// even with an empty comment thread.
	if err := writeAgentReservation(path, agentReservation{
		Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number, At: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed local hold: %v", err)
	}
	if err := r.precheckReservation(context.Background(), "lbl", clean, false); err == nil ||
		!strings.Contains(err.Error(), "reserved locally") {
		t.Fatalf("precheckReservation: want a local-reservation refusal, got %v", err)
	}
}

func TestPrecheckReservationLogs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

// TestReservationCommentBodyIsRoadBlock pins ward#494: the reservation comment carries
// the explicit "do not comment/edit to steer the run" road-block directive.
func TestReservationCommentBodyIsRoadBlock(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	body := reservationCommentBody(modeClaude, "engineer-claude-ward-494", "box", now, "", nil)
	if visible := visibleLinesBeforeDetails(body); visible != "WARD-RESERVATION: held 🔒" {
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
	listComments []issueComment
	listErr      error
	listCalls    int
}

func (f *fakeLockForge) getIssue(context.Context, string, string, int) (*dispatch.Issue, error) {
	return &dispatch.Issue{}, nil
}
func (f *fakeLockForge) listIssueComments(context.Context, string, string, int) ([]issueComment, error) {
	f.listCalls++
	return f.listComments, f.listErr
}
func (f *fakeLockForge) createIssue(context.Context, string, string, string, string) (int, error) {
	return 0, nil
}
func (f *fakeLockForge) commentIssue(_ context.Context, _, _ string, _ int, body string) error {
	if f.commentErr != nil {
		return f.commentErr
	}
	f.comments = append(f.comments, body)
	return nil
}
func (f *fakeLockForge) closeIssue(context.Context, string, string, int) error  { return nil }
func (f *fakeLockForge) reopenIssue(context.Context, string, string, int) error { return nil }
func (f *fakeLockForge) lockIssue(context.Context, string, string, int) error {
	f.locked++
	return f.lockErr
}
func (f *fakeLockForge) unlockIssue(context.Context, string, string, int) error {
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
		f := &fakeLockForge{}
		out := captureTestStderr(t, func() {
			r.releaseRemoteReservation(context.Background(), f, "lbl", modeClaude, ref, "engineer-claude-ward-570")
		})
		if len(f.comments) != 1 || !strings.Contains(f.comments[0], agentReservationReleaseMarker) {
			t.Fatalf("want one release-marker comment, got %v", f.comments)
		}
		if f.unlocked != 1 {
			t.Fatalf("unlockIssue called %d times, want 1", f.unlocked)
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

// TestForgejoLockUnsupported asserts the Forgejo client reports no lock leaf, the
// signal lockReservedIssue downgrades to the comment road-block (ward#494).
func TestForgejoLockUnsupported(t *testing.T) {
	c := &forgejoClient{}
	if err := c.lockIssue(context.Background(), "o", "r", 1); !errors.Is(err, errForgeLockUnsupported) {
		t.Errorf("forgejo lockIssue = %v, want errForgeLockUnsupported", err)
	}
	if err := c.unlockIssue(context.Background(), "o", "r", 1); !errors.Is(err, errForgeLockUnsupported) {
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

func TestReservationCommentBodyHasMarker(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	body := reservationCommentBody(modeCodex, "engineer-codex-ward-142", "tower", now, "", nil)
	for _, want := range []string{agentReservationMarker, "WARD-RESERVATION: held", "ward agent --harness codex", "engineer-codex-ward-142", "tower", "1h TTL"} {
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
	// A nil context renders nothing (a --force path with no captured context).
	if s := (*reservationSeedContext)(nil).render(); s != "" {
		t.Errorf("nil render should be empty, got %q", s)
	}
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
	if visible := visibleLinesBeforeDetails(generic); visible != "WARD-RESERVATION: released 🛑" {
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
	if visible := visibleLinesBeforeDetails(enriched); visible != "WARD-RESERVATION: released 🛑" {
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
		{"model-config", "model-config pre-launch gate", "Update the fleet model string"},
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
