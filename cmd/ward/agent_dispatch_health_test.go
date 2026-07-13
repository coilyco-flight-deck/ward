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
		RecentDispatches: 5,
		DuplicateRefs:    []string{"coilyco-flight-deck/ward#9×2"},
		Backpressure:     true,
		Runaway:          true,
		Signals:          []string{"deferred", "failed", "double-dispatch", "backpressure", "runaway"},
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
	report := dispatchHealthReport{StalePrelaunch: 2, LaunchIntents: 3, Signals: []string{"stale-prelaunch"}}
	line := report.summaryLine()
	if !strings.Contains(line, "stale-prelaunch=2") {
		t.Fatalf("summary line missing stale-prelaunch count: %s", line)
	}
	if !strings.Contains(line, "launch-intents=3") {
		t.Fatalf("summary line missing launch-intents count: %s", line)
	}
	if !strings.Contains(strings.Join(dispatchHealthSignals(report), ","), "stale-prelaunch") {
		t.Fatal("stale prelaunch reservations should surface as a signal")
	}
}
