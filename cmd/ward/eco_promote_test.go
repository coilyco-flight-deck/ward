package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeEcoExecutor records call order and returns scripted results, so the promote
// state machine's ordering + rollback logic is verified without a live server.
type fakeEcoExecutor struct {
	calls []string

	snapshotID  string
	snapshotErr error
	applyErr    error
	restartErr  error
	rollbackErr error

	// probes are returned in order, one per Probe call; probeErrs parallels it.
	probes    []ecoHealth
	probeErrs []error
	probeIdx  int
}

func (f *fakeEcoExecutor) Snapshot(context.Context) (string, error) {
	f.calls = append(f.calls, "snapshot")
	id := f.snapshotID
	if id == "" {
		id = "snap-1"
	}
	return id, f.snapshotErr
}

func (f *fakeEcoExecutor) ApplyMod(context.Context) error {
	f.calls = append(f.calls, "apply")
	return f.applyErr
}

func (f *fakeEcoExecutor) Restart(context.Context) error {
	f.calls = append(f.calls, "restart")
	return f.restartErr
}

func (f *fakeEcoExecutor) Probe(context.Context) (ecoHealth, error) {
	f.calls = append(f.calls, "probe")
	i := f.probeIdx
	f.probeIdx++
	var h ecoHealth
	if i < len(f.probes) {
		h = f.probes[i]
	}
	var err error
	if i < len(f.probeErrs) {
		err = f.probeErrs[i]
	}
	return h, err
}

func (f *fakeEcoExecutor) Rollback(_ context.Context, snapshotID string) error {
	f.calls = append(f.calls, "rollback:"+snapshotID)
	return f.rollbackErr
}

var noSleep = func(time.Duration) {}

func healthy() ecoHealth {
	return ecoHealth{ServiceActive: true, JournalClean: true, ServerReady: true}
}

// A healthy boot promotes and takes exactly the initial + canary samples.
func TestRunPromote_HealthyPromotes(t *testing.T) {
	f := &fakeEcoExecutor{
		probes: []ecoHealth{healthy(), healthy(), healthy(), healthy()},
	}
	res, err := runPromote(context.Background(), promotePlan{CanarySamples: 3, CanaryInterval: time.Second}, f, nil, noSleep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != outcomePromoted {
		t.Fatalf("want promoted, got %s (%s)", res.Outcome, res.Reason)
	}
	// snapshot BEFORE apply, then restart, then 1 initial + 3 canary probes.
	wantPrefix := []string{"snapshot", "apply", "restart", "probe", "probe", "probe", "probe"}
	if !equalStrs(f.calls, wantPrefix) {
		t.Fatalf("call order = %v, want %v", f.calls, wantPrefix)
	}
}

// Snapshot must always precede any mutation.
func TestRunPromote_SnapshotFirst(t *testing.T) {
	f := &fakeEcoExecutor{probes: []ecoHealth{healthy(), healthy()}}
	_, _ = runPromote(context.Background(), promotePlan{CanarySamples: 1}, f, nil, noSleep)
	if len(f.calls) == 0 || f.calls[0] != "snapshot" {
		t.Fatalf("first call must be snapshot, got %v", f.calls)
	}
}

// A snapshot failure aborts without touching the live server.
func TestRunPromote_SnapshotFailureAborts(t *testing.T) {
	f := &fakeEcoExecutor{snapshotErr: errors.New("disk full")}
	res, err := runPromote(context.Background(), promotePlan{}, f, nil, noSleep)
	if err != nil {
		t.Fatalf("abort should not surface a Go error: %v", err)
	}
	if res.Outcome != outcomeAborted {
		t.Fatalf("want aborted, got %s", res.Outcome)
	}
	if !equalStrs(f.calls, []string{"snapshot"}) {
		t.Fatalf("live server must be untouched after snapshot failure, calls=%v", f.calls)
	}
	if !res.ok() {
		t.Fatalf("an untouched abort is a safe outcome")
	}
}

// An apply failure rolls back with the snapshot id.
func TestRunPromote_ApplyFailureRollsBack(t *testing.T) {
	f := &fakeEcoExecutor{
		snapshotID: "snap-42",
		applyErr:   errors.New("scp refused"),
		probes:     []ecoHealth{healthy()}, // post-rollback verify probe
	}
	res, _ := runPromote(context.Background(), promotePlan{}, f, nil, noSleep)
	if res.Outcome != outcomeRolledBack {
		t.Fatalf("want rolled-back, got %s (%s)", res.Outcome, res.Reason)
	}
	assertContains(t, f.calls, "rollback:snap-42")
	// restart/probe must not run before the failed apply.
	if indexOf(f.calls, "restart") >= 0 && indexOf(f.calls, "restart") < indexOf(f.calls, "apply") {
		t.Fatalf("restart ran before apply: %v", f.calls)
	}
}

// An unhealthy initial probe rolls back.
func TestRunPromote_InitialProbeDegradedRollsBack(t *testing.T) {
	f := &fakeEcoExecutor{
		probes: []ecoHealth{
			{ServiceActive: true, JournalClean: false, ServerReady: true}, // degraded: dirty journal
			healthy(), // post-rollback verify
		},
	}
	res, _ := runPromote(context.Background(), promotePlan{CanarySamples: 3}, f, nil, noSleep)
	if res.Outcome != outcomeRolledBack {
		t.Fatalf("want rolled-back on degraded initial probe, got %s", res.Outcome)
	}
	assertContains(t, f.calls, "rollback:snap-1")
}

// A canary that degrades mid-window rolls back early (does not run all samples).
func TestRunPromote_CanaryDegradesRollsBackEarly(t *testing.T) {
	f := &fakeEcoExecutor{
		probes: []ecoHealth{
			healthy(), // initial
			healthy(), // canary 1
			{ServiceActive: false, JournalClean: true, ServerReady: false}, // canary 2 degraded
			healthy(), // post-rollback verify
		},
	}
	res, _ := runPromote(context.Background(), promotePlan{CanarySamples: 5, CanaryInterval: time.Second}, f, nil, noSleep)
	if res.Outcome != outcomeRolledBack {
		t.Fatalf("want rolled-back, got %s", res.Outcome)
	}
	// initial + 2 canary probes + 1 post-rollback verify = 4 probes; NOT 5 canary samples.
	if got := countStr(f.calls, "probe"); got != 4 {
		t.Fatalf("want early rollback after 4 probes, got %d: %v", got, f.calls)
	}
}

// A rollback that itself fails to recover is the loud, human-facing outcome.
func TestRunPromote_RollbackRestoreFailsLoud(t *testing.T) {
	f := &fakeEcoExecutor{
		applyErr:    errors.New("apply broke"),
		rollbackErr: errors.New("restore failed"),
	}
	res, _ := runPromote(context.Background(), promotePlan{}, f, nil, noSleep)
	if res.Outcome != outcomeRollbackFailed {
		t.Fatalf("want rollback-failed, got %s", res.Outcome)
	}
	if res.ok() {
		t.Fatalf("rollback-failed must not read as ok()")
	}
}

// A rollback that restores but comes back unhealthy is also loud.
func TestRunPromote_RollbackUnhealthyAfterRestoreFailsLoud(t *testing.T) {
	f := &fakeEcoExecutor{
		applyErr: errors.New("apply broke"),
		probes:   []ecoHealth{{ServiceActive: false}}, // post-rollback verify unhealthy
	}
	res, _ := runPromote(context.Background(), promotePlan{}, f, nil, noSleep)
	if res.Outcome != outcomeRollbackFailed {
		t.Fatalf("want rollback-failed on unhealthy restore, got %s", res.Outcome)
	}
}

func TestEcoHealth_DegradedReason(t *testing.T) {
	cases := []struct {
		h    ecoHealth
		want string
	}{
		{healthy(), ""},
		{ecoHealth{ServiceActive: false, JournalClean: true, ServerReady: true}, "systemd unit not active"},
		{ecoHealth{ServiceActive: true, JournalClean: true, ServerReady: false}, "server not ready"},
		{ecoHealth{ServiceActive: true, JournalClean: false, ServerReady: true}, "ModKit load exception in journal (eco-ops#7)"},
	}
	for _, c := range cases {
		if got := c.h.degradedReason(); got != c.want {
			t.Errorf("degradedReason(%+v) = %q, want %q", c.h, got, c.want)
		}
	}
}

func TestParseHealthOutput(t *testing.T) {
	h := parseHealthOutput("service_active=1 journal_clean=1\nserver_ready=0")
	if !h.ServiceActive || !h.JournalClean || h.ServerReady {
		t.Fatalf("parse mismatch: %+v", h)
	}
	if h.healthy() {
		t.Fatalf("server_ready=0 should not be healthy")
	}
}

// --- small helpers ---

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func countStr(xs []string, s string) int {
	n := 0
	for _, x := range xs {
		if x == s {
			n++
		}
	}
	return n
}

func assertContains(t *testing.T, xs []string, s string) {
	t.Helper()
	if indexOf(xs, s) < 0 {
		t.Fatalf("calls %v missing %q", xs, s)
	}
}
