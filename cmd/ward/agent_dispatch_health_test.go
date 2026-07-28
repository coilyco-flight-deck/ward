package main

import (
	"strings"
	"testing"
)

func TestDispatchHealthReportSummaryLine(t *testing.T) {
	report := dispatchHealthReport{
		Scope:            []string{"coilyco-flight-deck/ward"},
		Queued:           3,
		InFlight:         2,
		Held:             1,
		Submitted:        4,
		MergeReady:       1,
		Deferred:         2,
		Failed:           1,
		Running:          2,
		LaunchIntents:    1,
		CleanupNeeded:    4,
		FailedBefore:     2,
		RecentDispatches: 5,
		DuplicateRefs:    []string{"coilyco-flight-deck/ward#9×2"},
		Backpressure:     true,
		Runaway:          true,
		Signals:          []string{"deferred", "failed", "stale-records", "double-dispatch", "backpressure", "runaway"},
	}
	line := report.summaryLine()
	for _, want := range []string{
		"dispatch-health:",
		"queued=3",
		"inflight=2",
		"held=1",
		"deferred=2",
		"failed=1",
		"double-dispatch=coilyco-flight-deck/ward#9×2",
		"backpressure=on",
		"runaway=on",
		"launch-intents=1",
		"cleanup-needed=4",
		"failed-before-start=2",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("summary line missing %q:\n%s", want, line)
		}
	}
	if got := report.alertKey(); got == "" || !strings.Contains(got, "deferred") {
		t.Fatalf("alert key not stable enough: %q", got)
	}
	if !report.alertable() {
		t.Fatal("report with signals should be alertable")
	}
}

func TestDispatchHealthReportFlagsStalePrelaunch(t *testing.T) {
	report := dispatchHealthReport{StalePrelaunch: 2, LaunchIntents: 3, CleanupNeeded: 1, FailedBefore: 1, Signals: []string{"stale-prelaunch", "stale-records"}}
	line := report.summaryLine()
	if !strings.Contains(line, "stale-prelaunch=2") {
		t.Fatalf("summary line missing stale-prelaunch count: %s", line)
	}
	if !strings.Contains(line, "launch-intents=3") {
		t.Fatalf("summary line missing launch-intents count: %s", line)
	}
	if !strings.Contains(line, "cleanup-needed=1") || !strings.Contains(line, "failed-before-start=1") {
		t.Fatalf("summary line missing stale record counts: %s", line)
	}
	if !strings.Contains(strings.Join(dispatchHealthSignals(report), ","), "stale-prelaunch") {
		t.Fatal("stale prelaunch reservations should surface as a signal")
	}
	if !strings.Contains(strings.Join(dispatchHealthSignals(report), ","), "stale-records") {
		t.Fatal("cleanup-needed or failed-before-start launches should surface as a signal")
	}
}

func TestDispatchHealthReportFlagsPartialLaunch(t *testing.T) {
	report := dispatchHealthReport{Running: 1, PartialLaunch: 1, Signals: []string{"partial-launch"}}
	line := report.summaryLine()
	if !strings.Contains(line, "partial-launch=1") {
		t.Fatalf("summary line missing partial-launch count: %s", line)
	}
	if !strings.Contains(strings.Join(dispatchHealthSignals(report), ","), "partial-launch") {
		t.Fatal("partial launches should surface as a signal")
	}
	if !report.alertable() {
		t.Fatal("partial launches should make the report alertable")
	}
}

func TestDispatchHealthSkipsClosedCompletedIssueRefs(t *testing.T) {
	comments := []issueComment{
		{Body: "WARD-WORKFLOW: blocked 🛑\n\n<details><summary>details</summary>\n\nstalled\n\n</details>"},
		{Body: "WARD-WORKFLOW: done ✅\n\n<details><summary>details</summary>\n\nlanded on main\n\n</details>"},
		{Body: "WARD-WORKFLOW: failed ❌\n\n<details><summary>details</summary>\n\nfalse salvage noise\n\n</details>"},
	}
	if !dispatchHealthIssueHasDoneOutcome(comments) {
		t.Fatal("done marker should win over older or newer blocked/salvage noise")
	}

	activeIssue := func(ref agentIssueRef) bool {
		return ref.Number != 1443 && ref.Number != 1526
	}
	scope := map[string]bool{"coilyco-flight-deck/ward": true}
	rows := []agentRunningEngineer{
		{Repo: "coilyco-flight-deck/ward", Issue: "1443", Phase: agentLaunchPhaseRunning, Status: "running"},
		{Repo: "coilyco-flight-deck/ward", Issue: "1526", Phase: agentLaunchPhaseFailed, Status: agentLaunchStatusCleanup},
		{Repo: "coilyco-flight-deck/ward", Issue: "2000", Phase: agentLaunchPhaseRunning, Status: "running"},
		{Repo: "coilyco-flight-deck/ward", Issue: "2001", Phase: agentLaunchPhaseFailed, Status: "failed"},
	}
	visibleRows := dispatchHealthVisibleRows(rows, scope, activeIssue)
	if got := len(visibleRows); got != 2 {
		t.Fatalf("visible rows = %d, want 2 active issue rows", got)
	}
	inv := agentLaunchInventoryFromRowsWithScope(visibleRows, scope)
	if inv.Running != 1 || inv.CleanupNeeded != 0 || inv.FailedBefore != 1 || inv.LaunchIntents != 0 {
		t.Fatalf("inventory = %+v, want one running and one failed-before row after filtering", inv)
	}
	runningRows := dispatchHealthRunningRows(visibleRows)
	if got := len(runningRows); got != 1 || runningRows[0].Issue != "2000" {
		t.Fatalf("running rows = %+v, want only the active running issue", runningRows)
	}

	report := dispatchHealthReport{}
	entries := []*backlogEntry{
		{Num: 1443, Kind: backlogKindIssue, Lane: "headless", State: "blocked", repo: "coilyco-flight-deck/ward", LastOutcome: &backlogOutcome{Status: "blocked"}},
		{Num: 1526, Kind: backlogKindIssue, Lane: "headless", State: "queued", repo: "coilyco-flight-deck/ward", LastOutcome: &backlogOutcome{Status: "deferred"}},
		{Num: 2000, Kind: backlogKindIssue, Lane: "headless", State: "queued", repo: "coilyco-flight-deck/ward"},
		{Num: 2001, Kind: backlogKindIssue, Lane: "headless", State: "blocked", repo: "coilyco-flight-deck/ward"},
		{Num: 9001, Kind: backlogKindPullRequest, Lane: backlogKindPullRequest, State: "blocked", repo: "coilyco-flight-deck/ward", LastOutcome: &backlogOutcome{Status: "failed"}},
	}
	dispatchHealthTallyEntries(&report, entries, activeIssue)
	if report.Queued != 1 || report.Failed != 2 {
		t.Fatalf("report = %+v, want one active queued issue and two active failures (one issue, one PR)", report)
	}
	if report.Deferred != 0 || report.Submitted != 0 || report.MergeReady != 0 {
		t.Fatalf("report should not count filtered historical states, got %+v", report)
	}

	stale := []stalePrelaunchReservation{
		{Reservation: agentReservation{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1443}},
		{Reservation: agentReservation{Owner: "coilyco-flight-deck", Repo: "ward", Number: 2000}},
	}
	filtered := dispatchHealthStalePrelaunchReservations(stale, activeIssue)
	if got := len(filtered); got != 1 || filtered[0].Reservation.Number != 2000 {
		t.Fatalf("filtered stale prelaunch = %+v, want only the active issue hold", filtered)
	}
}
