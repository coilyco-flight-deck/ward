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
		Container: "long-dead", At: now.Add(-3 * agentReservationTTL),
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
	reserved := mk(reservationCommentBody(modeClaude, "ward-x", "box", now.Add(-time.Minute), ""), time.Minute, "coilyco-ops")
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
	reserved := reservationCommentBody(modeClaude, "ward-x", "box", now.Add(-30*time.Minute), "")

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

	release := reservationReleaseCommentBody(modeClaude, "ward-x")

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
	body := reservationCommentBody(modeClaude, "engineer-claude-ward-494", "box", now, "")
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

// fakeLockForge is a no-op issueForge whose lock/unlock return a configurable error,
// so lockReservedIssue's three branches can be exercised (ward#494).
type fakeLockForge struct {
	lockErr   error
	locked    int
	unlockErr error
	unlocked  int
}

func (f *fakeLockForge) getIssue(context.Context, string, string, int) (*dispatch.Issue, error) {
	return &dispatch.Issue{}, nil
}
func (f *fakeLockForge) listIssueComments(context.Context, string, string, int) ([]issueComment, error) {
	return nil, nil
}
func (f *fakeLockForge) createIssue(context.Context, string, string, string, string) (int, error) {
	return 0, nil
}
func (f *fakeLockForge) commentIssue(context.Context, string, string, int, string) error { return nil }
func (f *fakeLockForge) closeIssue(context.Context, string, string, int) error           { return nil }
func (f *fakeLockForge) reopenIssue(context.Context, string, string, int) error          { return nil }
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
	body := reservationCommentBody(modeCodex, "engineer-codex-ward-142", "tower", now, "")
	for _, want := range []string{agentReservationMarker, "ward agent --driver codex", "engineer-codex-ward-142", "tower"} {
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
	body := reservationCommentBody(modeClaude, "engineer-claude-ward-383", "box", now, "  "+read+"  ")
	for _, want := range []string{"pre-flight read (GO)", "<details>", read, "**GO**"} {
		if !strings.Contains(body, want) {
			t.Errorf("reservation comment missing %q\n got: %s", want, body)
		}
	}
	// The marker still leads so freshReservationComment recognizes it as a hold.
	if !strings.HasPrefix(body, agentReservationMarker) {
		t.Errorf("reservation comment lost its leading marker\n got: %s", body)
	}
}
