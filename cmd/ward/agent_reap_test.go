package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestLastDockerLogTime parses the newest timestamp from --timestamps log output,
// tolerating blank lines and a body that itself contains spaces.
func TestLastDockerLogTime(t *testing.T) {
	logs := "2026-07-03T10:00:00.000000000Z first tool call\n" +
		"\n" +
		"2026-07-03T10:05:30.500000000Z ran make test with spaces\n"
	got, ok := lastDockerLogTime(logs)
	if !ok {
		t.Fatal("expected a parseable timestamp")
	}
	want := time.Date(2026, 7, 3, 10, 5, 30, 500000000, time.UTC)
	if !got.Equal(want) {
		t.Errorf("newest = %s; want %s", got, want)
	}
}

// TestLastDockerLogTimeNoneParse returns ok=false when nothing carries a timestamp
// (an empty log, or a container that has produced no output).
func TestLastDockerLogTimeNoneParse(t *testing.T) {
	for _, in := range []string{"", "   \n\n", "not-a-timestamp line one\nstill not\n"} {
		if _, ok := lastDockerLogTime(in); ok {
			t.Errorf("lastDockerLogTime(%q) = ok; want no parse", in)
		}
	}
}

// TestParseDockerInspectTime parses a StartedAt and rejects the zero-year value
// docker reports for a not-yet-started container.
func TestParseDockerInspectTime(t *testing.T) {
	if _, ok := parseDockerInspectTime("2026-07-03T09:00:00.123456789Z"); !ok {
		t.Error("expected a valid StartedAt to parse")
	}
	for _, in := range []string{"", "0001-01-01T00:00:00Z", "garbage"} {
		if _, ok := parseDockerInspectTime(in); ok {
			t.Errorf("parseDockerInspectTime(%q) = ok; want no parse", in)
		}
	}
}

// TestParseCPUPercent reads the docker-stats %CPU cell and rejects a blank one.
func TestParseCPUPercent(t *testing.T) {
	cases := map[string]struct {
		want float64
		ok   bool
	}{
		"12.34%":   {12.34, true},
		"  0.00% ": {0, true},
		"250.5%":   {250.5, true},
		"":         {0, false},
		"--":       {0, false},
		"%":        {0, false},
	}
	for in, exp := range cases {
		got, ok := parseCPUPercent(in)
		if ok != exp.ok || (ok && got != exp.want) {
			t.Errorf("parseCPUPercent(%q) = (%v, %v); want (%v, %v)", in, got, ok, exp.want, exp.ok)
		}
	}
}

// TestIdleSinceClampsSkew clamps a clock-skew negative idle to zero.
func TestIdleSinceClampsSkew(t *testing.T) {
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	if d := idleSince(now, now.Add(time.Minute)); d != 0 {
		t.Errorf("future last-activity gave idle %s; want 0", d)
	}
	if d := idleSince(now, now.Add(-90*time.Second)); d != 90*time.Second {
		t.Errorf("idleSince = %s; want 90s", d)
	}
}

// TestReapVerdict covers the stop/keep decision across the idle threshold and the
// CPU guard, including the guard-only-spares invariant when CPU is unreadable.
func TestReapVerdict(t *testing.T) {
	const threshold = time.Hour
	const maxCPU = 5.0

	tests := []struct {
		name     string
		st       engineerReapState
		wantStop bool
	}{
		{
			name:     "idle unknown is never stopped",
			st:       engineerReapState{HasIdle: false},
			wantStop: false,
		},
		{
			name:     "below threshold is kept",
			st:       engineerReapState{Idle: 30 * time.Minute, HasIdle: true, CPU: 0, HasCPU: true},
			wantStop: false,
		},
		{
			name:     "idle past threshold, low CPU, stopped",
			st:       engineerReapState{Idle: 2 * time.Hour, HasIdle: true, CPU: 0.5, HasCPU: true},
			wantStop: true,
		},
		{
			name:     "idle past threshold but busy CPU spares it",
			st:       engineerReapState{Idle: 2 * time.Hour, HasIdle: true, CPU: 80, HasCPU: true},
			wantStop: false,
		},
		{
			name:     "idle past threshold, CPU unreadable, still stopped (guard only spares)",
			st:       engineerReapState{Idle: 2 * time.Hour, HasIdle: true, HasCPU: false},
			wantStop: true,
		},
		{
			name:     "exactly at threshold is stopped",
			st:       engineerReapState{Idle: threshold, HasIdle: true, HasCPU: false},
			wantStop: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stop, reason := reapVerdict(tc.st, threshold, maxCPU)
			if stop != tc.wantStop {
				t.Errorf("reapVerdict stop = %v; want %v (reason: %s)", stop, tc.wantStop, reason)
			}
			if reason == "" {
				t.Error("reapVerdict returned an empty reason")
			}
		})
	}
}

// TestAgentReapCommandRegistered asserts `reap` mounts under the agent umbrella and
// is treated as a meta verb the roster skips (not a startup role).
func TestAgentReapCommandRegistered(t *testing.T) {
	if commandNamed(agentCommand().Commands, "reap") == nil {
		t.Fatalf("agent umbrella missing the reap command; got %v", commandNames(agentCommand().Commands))
	}
	if !agentMetaCommands["reap"] {
		t.Error("reap must be an agent meta command so the roster skips it")
	}
	// It must not appear as a roster role (that path requires a descriptor).
	rows, err := agentRosterRows()
	if err != nil {
		t.Fatalf("agentRosterRows: %v", err)
	}
	for _, r := range rows {
		if r.Role == "reap" {
			t.Error("reap leaked into the role roster; it is a maintenance verb")
		}
	}
}

// TestAgentLogsCommandRegistered asserts `logs` mounts under the agent umbrella and
// is treated as a meta verb the roster skips (not a startup role).
func TestAgentLogsCommandRegistered(t *testing.T) {
	if commandNamed(agentCommand().Commands, "logs") == nil {
		t.Fatalf("agent umbrella missing the logs command; got %v", commandNames(agentCommand().Commands))
	}
	if !agentMetaCommands["logs"] {
		t.Error("logs must be an agent meta command so the roster skips it")
	}
	rows, err := agentRosterRows()
	if err != nil {
		t.Fatalf("agentRosterRows: %v", err)
	}
	for _, r := range rows {
		if r.Role == "logs" {
			t.Error("logs leaked into the role roster; it is a maintenance verb")
		}
	}
}

// TestAgentReapSweepReportsUnsupportedOnReadOnlyDirectorSurface keeps the read-only
// director error honest when the Docker socket is intentionally unavailable.
func TestAgentReapSweepReportsUnsupportedOnReadOnlyDirectorSurface(t *testing.T) {
	t.Setenv("WARD_READONLY", "1")
	r := &Runner{Runner: &shell.Runner{Resolve: func(bin string) (string, error) {
		if bin == "docker" {
			return "", errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
		}
		return "/bin/true", nil
	}}}
	err := r.agentReapSweep(context.Background(), time.Hour, 5, false, true, io.Discard)
	if err == nil {
		t.Fatal("expected an unsupported-surface error")
	}
	if got := err.Error(); !strings.Contains(got, "reaping is unsupported on this read-only director surface") {
		t.Fatalf("error = %q, want a read-only surface unsupported message", got)
	}
}
