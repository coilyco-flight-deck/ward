package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

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
		Mode: "claude", Container: "ward-ward-issue-142-claude-abcd",
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
	reserved := mk(reservationCommentBody(modeClaude, "ward-x", "box", now.Add(-time.Minute)), time.Minute, "coilyco-ops")
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

func TestFreshReservationComment(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	ttl := 2 * time.Hour
	mk := func(body string, age time.Duration, login string) issueComment {
		c := issueComment{Body: body, CreatedAt: now.Add(-age)}
		c.User.Login = login
		return c
	}
	reserved := reservationCommentBody(modeClaude, "ward-x", "box", now.Add(-30*time.Minute))

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
}

func TestReservationCommentBodyHasMarker(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	body := reservationCommentBody(modeCodex, "ward-ward-issue-142-codex-abcd", "tower", now)
	for _, want := range []string{agentReservationMarker, "ward agent --driver codex", "ward-ward-issue-142-codex-abcd", "tower"} {
		if !strings.Contains(body, want) {
			t.Errorf("reservation comment missing %q\n got: %s", want, body)
		}
	}
}
