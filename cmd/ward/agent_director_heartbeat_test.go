package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDirectorDecision(t *testing.T) {
	cases := []struct {
		name     string
		read     string
		wantNums []int
		wantOK   bool
	}{
		{"no dispatch line", "I think we should wait a bit.", nil, false},
		{"plain list", "Dispatching the top two.\nDISPATCH: 5, 7", []int{5, 7}, true},
		{"hash + prose", "go for it\nDISPATCH: #5 and #7", []int{5, 7}, true},
		{"explicit hold none", "recent runs are failing.\nDISPATCH: none", nil, true},
		{"bulleted/decorated line", "- **DISPATCH:** 12", []int{12}, true},
		{"last line wins", "DISPATCH: 1\nactually:\nDISPATCH: 2, 3", []int{2, 3}, true},
		{"case-insensitive verdict", "dispatch: 9", []int{9}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nums, ok := parseDirectorDecision(c.read)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !reflect.DeepEqual(nums, c.wantNums) {
				t.Errorf("nums = %v, want %v", nums, c.wantNums)
			}
		})
	}
}

func TestSelectDirectorPicks(t *testing.T) {
	picks := []*backlogEntry{
		{Num: 5, Tier: "P0"},
		{Num: 7, Tier: "P1"},
		{Num: 9, Tier: "P2"},
	}
	cases := []struct {
		name  string
		nums  []int
		avail int
		want  []int
	}{
		{"subset preserves rank order", []int{9, 5}, 5, []int{5, 9}},
		{"capped at avail", []int{5, 7, 9}, 2, []int{5, 7}},
		{"hallucinated numbers ignored", []int{5, 42, 100}, 5, []int{5}},
		{"empty hold yields nothing", nil, 5, nil},
		{"duplicate request not double-counted", []int{7, 7}, 5, []int{7}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := selectDirectorPicks(picks, c.nums, c.avail)
			var nums []int
			for _, e := range got {
				nums = append(nums, e.Num)
			}
			if !reflect.DeepEqual(nums, c.want) {
				t.Errorf("got %v, want %v", nums, c.want)
			}
		})
	}
}

func TestDirectorDecidePrompt(t *testing.T) {
	picks := []*backlogEntry{{Num: 5, Tier: "P0", Title: "carry me"}}
	entries := []*backlogEntry{
		picks[0],
		{Num: 8, Tier: "P1", Title: "running", State: "dispatched"},
		{Num: 9, Tier: "P2", Title: "broke", State: "failed", LastOutcome: &backlogOutcome{Status: "failed", Text: "build failed"}},
	}
	got := directorDecidePrompt(picks, 2, entries)
	for _, want := range []string{
		"at most 2 issue(s)", // the free-slot budget
		"#5",                 // the queued candidate
		"IN FLIGHT",          // the dispatched section
		"#8",                 // the in-flight issue
		"RECENT OUTCOMES",    // the outcome section
		"build failed",       // the failure text the agent should weigh
		"DISPATCH:",          // the verdict contract
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, got)
		}
	}
}

// fakeDirector is an in-memory directorBackend: poll reconciles a dispatched entry to
// done, dispatch flips queued -> dispatched, surface is recorded - no docker/forgejo/LLM.
type fakeDirector struct {
	list          []*backlogEntry
	decideFn      func(picks []*backlogEntry, avail int) []*backlogEntry
	dispatched    []int
	surfaceFn     func() (bool, error)
	surfaceCalls  int
	drainedCalls  int
	maxCycleCalls int
	summaryCalls  int
	sleeps        int
}

func (f *fakeDirector) poll(context.Context) {
	for _, e := range f.list {
		if e.State == "dispatched" {
			e.State = "done"
			e.LastOutcome = &backlogOutcome{Status: "done", Text: "merged"}
		}
	}
}

func (f *fakeDirector) refresh(context.Context) {}

func (f *fakeDirector) entries() []*backlogEntry { return f.list }

func (f *fakeDirector) decide(_ context.Context, picks []*backlogEntry, avail int, _ []*backlogEntry) []*backlogEntry {
	if f.decideFn != nil {
		return f.decideFn(picks, avail)
	}
	floor := picks
	if len(floor) > avail {
		floor = floor[:avail]
	}
	return floor
}

func (f *fakeDirector) dispatch(_ context.Context, p *backlogEntry) error {
	f.dispatched = append(f.dispatched, p.Num)
	p.State = "dispatched"
	return nil
}

func (f *fakeDirector) surface(context.Context) (bool, error) {
	f.surfaceCalls++
	if f.surfaceFn != nil {
		return f.surfaceFn()
	}
	return true, nil
}

func (f *fakeDirector) sleep(context.Context, time.Duration) error { f.sleeps++; return nil }
func (f *fakeDirector) reportDrained() error                       { f.drainedCalls++; return nil }
func (f *fakeDirector) reportMaxCycles(int, int)                   { f.maxCycleCalls++ }
func (f *fakeDirector) summary() error                             { f.summaryCalls++; return nil }

// TestRunDirectorLoopSmoke is the acceptance smoke: one actionable issue dispatches,
// reconciles its WARD-OUTCOME next tick, then drains and surfaces (does not exit).
func TestRunDirectorLoopSmoke(t *testing.T) {
	issue := &backlogEntry{Num: 5, Title: "actionable", Tier: "P0", Lane: "headless", State: "queued"}
	f := &fakeDirector{list: []*backlogEntry{issue}}
	cfg := backlogConfig{maxParallel: 2, pollInterval: time.Millisecond}

	if err := runDirectorLoop(context.Background(), cfg, f); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if !reflect.DeepEqual(f.dispatched, []int{5}) {
		t.Errorf("dispatched = %v, want [5]", f.dispatched)
	}
	if issue.State != "done" {
		t.Errorf("issue should reconcile to done, got %q", issue.State)
	}
	if f.drainedCalls != 1 {
		t.Errorf("drained report count = %d, want 1", f.drainedCalls)
	}
	if f.surfaceCalls != 1 {
		t.Errorf("surface count = %d, want 1 (surfaces on drain, does not exit)", f.surfaceCalls)
	}
	if f.summaryCalls != 0 {
		t.Errorf("summary should not print on the surface path, got %d", f.summaryCalls)
	}
}

// TestRunDirectorLoopResumesOnRefill checks the surface hands control back and the
// heartbeat resumes when the human files new headless work, draining a second time.
func TestRunDirectorLoopResumesOnRefill(t *testing.T) {
	issue := &backlogEntry{Num: 5, Title: "first", Tier: "P0", Lane: "headless", State: "queued"}
	f := &fakeDirector{list: []*backlogEntry{issue}}
	refilled := false
	f.surfaceFn = func() (bool, error) {
		if !refilled {
			refilled = true
			f.list = append(f.list, &backlogEntry{Num: 6, Title: "second", Tier: "P1", Lane: "headless", State: "queued"})
		}
		return true, nil
	}
	cfg := backlogConfig{maxParallel: 2, pollInterval: time.Millisecond}

	if err := runDirectorLoop(context.Background(), cfg, f); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if !reflect.DeepEqual(f.dispatched, []int{5, 6}) {
		t.Errorf("dispatched = %v, want [5 6] (resumed after refill)", f.dispatched)
	}
	if f.surfaceCalls != 2 {
		t.Errorf("surface count = %d, want 2 (drain, refill+resume, drain again)", f.surfaceCalls)
	}
}

// TestRunDirectorLoopExitsWhenNoSurface confirms a non-interactive drain (surface
// unavailable) exits cleanly rather than spinning.
func TestRunDirectorLoopExitsWhenNoSurface(t *testing.T) {
	f := &fakeDirector{list: nil} // nothing queued or in flight: drained immediately
	f.surfaceFn = func() (bool, error) { return false, nil }
	cfg := backlogConfig{maxParallel: 2, pollInterval: time.Millisecond}

	if err := runDirectorLoop(context.Background(), cfg, f); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if f.drainedCalls != 1 || f.surfaceCalls != 1 {
		t.Errorf("drained=%d surface=%d, want 1/1", f.drainedCalls, f.surfaceCalls)
	}
	if f.dispatched != nil {
		t.Errorf("nothing should dispatch, got %v", f.dispatched)
	}
}

// dispoDirector is a directorBackend that applies the live ward#352 disposition to a
// per-issue injected error: the loop seam sees a conflict (defer) vs a real failure.
type dispoDirector struct {
	*fakeDirector
	errs map[int]error
}

func (d *dispoDirector) dispatch(ctx context.Context, p *backlogEntry) error {
	if err := d.errs[p.Num]; err != nil {
		state, outcome, _ := directorDispatchDisposition(err)
		p.State = state
		p.LastOutcome = outcome
		return nil
	}
	return d.fakeDirector.dispatch(ctx, p)
}

// TestRunDirectorLoopDefersReservationConflict covers ward#352: a reservation conflict
// leaves the issue queued/eligible, never failed, never dispatched (backend seam).
func TestRunDirectorLoopDefersReservationConflict(t *testing.T) {
	issue := &backlogEntry{Num: 5, Title: "held elsewhere", Tier: "P0", Lane: "headless", State: "queued"}
	d := &dispoDirector{
		fakeDirector: &fakeDirector{list: []*backlogEntry{issue}},
		errs:         map[int]error{5: newReservationConflict("issue a/b#5 is already reserved remotely")},
	}
	cfg := backlogConfig{maxParallel: 2, pollInterval: time.Millisecond, maxCycles: 2}

	if err := runDirectorLoop(context.Background(), cfg, d); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if issue.State != "queued" {
		t.Errorf("a deferred issue must stay queued/eligible, got %q", issue.State)
	}
	if d.dispatched != nil {
		t.Errorf("a deferred dispatch did not launch, so nothing should count as dispatched, got %v", d.dispatched)
	}
	if d.maxCycleCalls != 1 {
		t.Errorf("the loop should bound on --max-cycles (held lane never drains), maxCycle=%d", d.maxCycleCalls)
	}
}

// TestRunDirectorLoopParksRealFailure confirms the other half of ward#352: a real launch
// failure still parks the issue failed (then the lane drains and surfaces).
func TestRunDirectorLoopParksRealFailure(t *testing.T) {
	issue := &backlogEntry{Num: 5, Title: "real failure", Tier: "P0", Lane: "headless", State: "queued"}
	d := &dispoDirector{
		fakeDirector: &fakeDirector{list: []*backlogEntry{issue}},
		errs:         map[int]error{5: errors.New("image pull failed")},
	}
	d.surfaceFn = func() (bool, error) { return false, nil } // non-interactive: drain exits cleanly
	cfg := backlogConfig{maxParallel: 2, pollInterval: time.Millisecond, maxCycles: 5}

	if err := runDirectorLoop(context.Background(), cfg, d); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if issue.State != "failed" {
		t.Errorf("a genuine launch failure must park failed, got %q", issue.State)
	}
}

// TestRunDirectorLoopMaxCycles confirms --max-cycles stops a heartbeat whose LLM keeps
// holding (never draining), printing the summary instead of surfacing.
func TestRunDirectorLoopMaxCycles(t *testing.T) {
	issue := &backlogEntry{Num: 5, Title: "held", Tier: "P0", Lane: "headless", State: "queued"}
	f := &fakeDirector{list: []*backlogEntry{issue}}
	f.decideFn = func([]*backlogEntry, int) []*backlogEntry { return nil } // always hold
	cfg := backlogConfig{maxParallel: 1, pollInterval: time.Millisecond, maxCycles: 3}

	if err := runDirectorLoop(context.Background(), cfg, f); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	if f.dispatched != nil {
		t.Errorf("a held heartbeat dispatches nothing, got %v", f.dispatched)
	}
	if f.maxCycleCalls != 1 || f.summaryCalls != 1 {
		t.Errorf("maxCycle=%d summary=%d, want 1/1", f.maxCycleCalls, f.summaryCalls)
	}
	if f.surfaceCalls != 0 {
		t.Errorf("a non-drained max-cycles stop should not surface, got %d", f.surfaceCalls)
	}
}
