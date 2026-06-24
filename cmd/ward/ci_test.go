package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
)

// TestCIIsTerminal pins which Forgejo statuses settle the poll loop, including
// the case-folding the script's literal match lacked.
func TestCIIsTerminal(t *testing.T) {
	terminal := []string{"success", "failure", "failed", "cancelled", "canceled", "skipped", "error", "SUCCESS", " failure "}
	for _, s := range terminal {
		if !ciIsTerminal(s) {
			t.Errorf("ciIsTerminal(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"running", "waiting", "queued", "blocked", ""} {
		if ciIsTerminal(s) {
			t.Errorf("ciIsTerminal(%q) = true, want false", s)
		}
	}
}

// TestCIIsFailure pins that only failure/failed/error count as failures -
// cancelled and skipped are terminal but pass the run, like the script.
func TestCIIsFailure(t *testing.T) {
	for _, s := range []string{"failure", "failed", "error", "ERROR"} {
		if !ciIsFailure(s) {
			t.Errorf("ciIsFailure(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"success", "cancelled", "skipped", "running", ""} {
		if ciIsFailure(s) {
			t.Errorf("ciIsFailure(%q) = true, want false", s)
		}
	}
}

// TestCILatestRun resolves the highest run number, the latest-run default.
func TestCILatestRun(t *testing.T) {
	tasks := []ciTask{{RunNumber: 5}, {RunNumber: 7}, {RunNumber: 7}, {RunNumber: 3}}
	run, ok := ciLatestRun(tasks)
	if !ok || run != 7 {
		t.Errorf("ciLatestRun = (%d, %v), want (7, true)", run, ok)
	}
	if _, ok := ciLatestRun(nil); ok {
		t.Error("ciLatestRun(nil) ok = true, want false")
	}
}

// TestCIRunTasksAndCounts pins the per-run filter plus pending/failure counts.
func TestCIRunTasksAndCounts(t *testing.T) {
	tasks := []ciTask{
		{RunNumber: 7, Name: "build", Status: "success"},
		{RunNumber: 7, Name: "test", Status: "failure"},
		{RunNumber: 7, Name: "lint", Status: "running"},
		{RunNumber: 5, Name: "old", Status: "running"},
	}
	run7 := ciRunTasks(tasks, 7)
	if len(run7) != 3 {
		t.Fatalf("ciRunTasks(7) len = %d, want 3", len(run7))
	}
	if got := ciPending(run7); got != 1 {
		t.Errorf("ciPending = %d, want 1 (lint)", got)
	}
	if got := ciFailures(run7); len(got) != 1 || got[0].Name != "test" {
		t.Errorf("ciFailures = %v, want [test]", got)
	}
}

// fakeLister returns a queued sequence of task snapshots, one per poll, so a
// watcher test can model a run moving from running to terminal across polls.
type fakeLister struct {
	snaps [][]ciTask
	calls int
}

func (f *fakeLister) next(context.Context) ([]ciTask, error) {
	i := f.calls
	if i >= len(f.snaps) {
		i = len(f.snaps) - 1 // hold on the last snapshot
	}
	f.calls++
	return f.snaps[i], nil
}

// newTestWatcher builds a watcher over a fakeLister with an instant sleep and a
// discarded progress writer.
func newTestWatcher(snaps [][]ciTask, timeout time.Duration) (*ciWatcher, *int) {
	f := &fakeLister{snaps: snaps}
	sleeps := 0
	return &ciWatcher{
		list:     f.next,
		sleep:    func(time.Duration) { sleeps++ },
		interval: time.Second,
		timeout:  timeout,
		out:      &bytes.Buffer{},
	}, &sleeps
}

// TestWatchPollsUntilTerminal walks a run from running to success across two
// polls, then returns ciPassed.
func TestWatchPollsUntilTerminal(t *testing.T) {
	snaps := [][]ciTask{
		{{RunNumber: 7, Name: "build", Status: "running"}},
		{{RunNumber: 7, Name: "build", Status: "success"}},
	}
	w, sleeps := newTestWatcher(snaps, time.Minute)
	run, tasks, outcome, err := w.watch(context.Background(), 7)
	if err != nil {
		t.Fatalf("watch err = %v", err)
	}
	if run != 7 || outcome != ciPassed {
		t.Errorf("watch = (run %d, %v), want (7, ciPassed)", run, outcome)
	}
	if len(tasks) != 1 || tasks[0].Status != "success" {
		t.Errorf("tasks = %v, want one success", tasks)
	}
	if *sleeps != 1 {
		t.Errorf("slept %d times, want 1 (one running poll)", *sleeps)
	}
}

// TestWatchResolvesLatestRun confirms run 0 binds the highest run_number on the
// first poll rather than watching a stale run.
func TestWatchResolvesLatestRun(t *testing.T) {
	snaps := [][]ciTask{{
		{RunNumber: 5, Name: "old", Status: "success"},
		{RunNumber: 8, Name: "build", Status: "success"},
	}}
	w, _ := newTestWatcher(snaps, time.Minute)
	run, _, outcome, err := w.watch(context.Background(), 0)
	if err != nil {
		t.Fatalf("watch err = %v", err)
	}
	if run != 8 || outcome != ciPassed {
		t.Errorf("watch = (run %d, %v), want (8, ciPassed)", run, outcome)
	}
}

// TestWatchFailure surfaces ciFailed when a job ends in failure.
func TestWatchFailure(t *testing.T) {
	snaps := [][]ciTask{{
		{RunNumber: 7, Name: "build", Status: "success"},
		{RunNumber: 7, Name: "test", Status: "failure"},
	}}
	w, _ := newTestWatcher(snaps, time.Minute)
	_, _, outcome, _ := w.watch(context.Background(), 7)
	if outcome != ciFailed {
		t.Errorf("outcome = %v, want ciFailed", outcome)
	}
}

// TestWatchTimeout gives up while a job is still running once elapsed reaches
// the timeout.
func TestWatchTimeout(t *testing.T) {
	snaps := [][]ciTask{{{RunNumber: 7, Name: "build", Status: "running"}}}
	w, _ := newTestWatcher(snaps, 2*time.Second) // interval 1s -> times out on 3rd poll
	_, _, outcome, err := w.watch(context.Background(), 7)
	if err != nil {
		t.Fatalf("watch err = %v", err)
	}
	if outcome != ciTimedOut {
		t.Errorf("outcome = %v, want ciTimedOut", outcome)
	}
}

// TestWatchNoRun returns ciNoRun when the requested run is absent and when the
// listing is empty.
func TestWatchNoRun(t *testing.T) {
	w, _ := newTestWatcher([][]ciTask{{{RunNumber: 5, Status: "success"}}}, time.Minute)
	if _, _, outcome, _ := w.watch(context.Background(), 99); outcome != ciNoRun {
		t.Errorf("missing run outcome = %v, want ciNoRun", outcome)
	}
	w, _ = newTestWatcher([][]ciTask{{}}, time.Minute)
	if _, _, outcome, _ := w.watch(context.Background(), 0); outcome != ciNoRun {
		t.Errorf("empty listing outcome = %v, want ciNoRun", outcome)
	}
}

// TestWatchListError propagates a backend error rather than masking it as a
// terminal verdict.
func TestWatchListError(t *testing.T) {
	boom := errors.New("backend down")
	w := &ciWatcher{
		list:    func(context.Context) ([]ciTask, error) { return nil, boom },
		sleep:   func(time.Duration) {},
		timeout: time.Minute,
		out:     &bytes.Buffer{},
	}
	if _, _, _, err := w.watch(context.Background(), 7); !errors.Is(err, boom) {
		t.Errorf("watch err = %v, want backend down", err)
	}
}

// codeOf extracts the declared exit code from a coded error (0 when nil).
func codeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var coded exitcode.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not exitcode.Coded", err)
	}
	return coded.Code()
}

// TestReportCIOutcomeExitCodes pins the 0/1/2/3 contract the retired script
// documented, now carried by exitcode.Coded.
func TestReportCIOutcomeExitCodes(t *testing.T) {
	pass := []ciTask{{RunNumber: 7, Name: "build", Status: "success"}}
	fail := []ciTask{{RunNumber: 7, Name: "test", Status: "failure"}}
	cases := []struct {
		name    string
		tasks   []ciTask
		outcome ciOutcome
		code    int
		want    string
	}{
		{"passed", pass, ciPassed, 0, "all jobs passed."},
		{"failed", fail, ciFailed, 1, "failing jobs"},
		{"timedout", pass, ciTimedOut, 2, "run#7 on"},
		{"norun", nil, ciNoRun, 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := reportCIOutcome(&out, "coilyco-flight-deck", "ward", "coilyco-flight-deck/ward", 7, tc.tasks, tc.outcome)
			if got := codeOf(t, err); got != tc.code {
				t.Errorf("exit code = %d, want %d", got, tc.code)
			}
			if tc.want != "" && !strings.Contains(out.String(), tc.want) {
				t.Errorf("output %q missing %q", out.String(), tc.want)
			}
		})
	}
}

// TestCIFailureReportFallsBackToActionsURL pins that a job without an html_url
// still gets a clickable pointer (the repo Actions page).
func TestCIFailureReportFallsBackToActionsURL(t *testing.T) {
	got := ciFailureReport("coilyco-flight-deck", "ward", []ciTask{{Name: "test", Status: "failure", ID: 42}})
	wantURL := fmt.Sprintf("%s/coilyco-flight-deck/ward/actions", forgejoBaseURL)
	if !strings.Contains(got, wantURL) {
		t.Errorf("report %q missing fallback URL %q", got, wantURL)
	}
	withURL := ciFailureReport("o", "r", []ciTask{{Name: "test", Status: "failure", HTMLURL: "https://example/run/9"}})
	if !strings.Contains(withURL, "https://example/run/9") {
		t.Errorf("report %q dropped the task html_url", withURL)
	}
}

// TestCIStatusTableSorted pins deterministic, job-sorted status output.
func TestCIStatusTableSorted(t *testing.T) {
	tasks := []ciTask{{Name: "zeta", Status: "success"}, {Name: "alpha", Status: "failure"}}
	got := ciStatusTable("o/r", 7, tasks)
	if !strings.HasPrefix(got, "run#7 on o/r:\n") {
		t.Errorf("table head wrong: %q", got)
	}
	if strings.Index(got, "alpha") > strings.Index(got, "zeta") {
		t.Errorf("table not sorted by job name: %q", got)
	}
}

// TestCIWatchCommandShape pins the verb wiring: name, the latest-run default,
// and the WATCH_CI_* env sources carried over from the script.
func TestCIWatchCommandShape(t *testing.T) {
	root := ciCommand()
	if root.Name != "ci" || len(root.Commands) == 0 || root.Commands[0].Name != "watch" {
		t.Fatalf("ci command shape wrong: %+v", root)
	}
	watch := root.Commands[0]
	envByFlag := map[string]string{"interval": "WATCH_CI_INTERVAL", "timeout": "WATCH_CI_TIMEOUT", "limit": "WATCH_CI_LIMIT"}
	for _, f := range watch.Flags {
		name := f.Names()[0]
		want, ok := envByFlag[name]
		if !ok {
			continue
		}
		if !strings.Contains(strings.Join(f.(interface{ GetEnvVars() []string }).GetEnvVars(), ","), want) {
			t.Errorf("flag %q missing env source %q", name, want)
		}
	}
}
