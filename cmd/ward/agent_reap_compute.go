package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// agent_reap_compute.go is the pure core of `ward agent reap` (issue #376): the
// idle-signal parsing + stop/keep verdict, split from the docker I/O for testing.

// engineerReapState is one engineer's idle inputs: idle since last activity + its
// CPU, each with a readable flag so an unreadable probe is not a hard zero.
type engineerReapState struct {
	Name    string
	Idle    time.Duration
	HasIdle bool // false when neither a timestamped log line nor a start time parsed
	CPU     float64
	HasCPU  bool // false when `docker stats` did not yield a parseable %CPU
}

// lastDockerLogTime returns the newest RFC3339Nano-prefixed line time from `docker
// logs --timestamps` output; ok is false when no line carried a parseable timestamp.
func lastDockerLogTime(logs string) (time.Time, bool) {
	var newest time.Time
	var found bool
	for _, line := range strings.Split(logs, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The RFC3339Nano stamp is the first whitespace-delimited token; the log body
		// (which may itself contain spaces) follows it.
		tok := line
		if i := strings.IndexByte(line, ' '); i >= 0 {
			tok = line[:i]
		}
		t, err := time.Parse(time.RFC3339Nano, tok)
		if err != nil {
			continue
		}
		if !found || t.After(newest) {
			newest, found = t, true
		}
	}
	return newest, found
}

// parseDockerInspectTime parses the `docker inspect -f {{.State.StartedAt}}` value
// (RFC3339Nano); ok is false on an empty/unparseable/zero-value time.
func parseDockerInspectTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.IsZero() {
		return time.Time{}, false
	}
	// Docker reports a not-yet-started container as the Go zero year 0001.
	if t.Year() <= 1 {
		return time.Time{}, false
	}
	return t, true
}

// parseCPUPercent reads a `docker stats --format {{.CPUPerc}}` cell like "12.34%";
// ok is false when the cell is blank or unparseable (a raced-away container).
func parseCPUPercent(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// idleSince computes the idle duration from now back to the last-activity time,
// clamping a clock-skew negative to zero so display and comparison stay sane.
func idleSince(now, last time.Time) time.Duration {
	d := now.Sub(last)
	if d < 0 {
		return 0
	}
	return d
}

// reapVerdict stops one engineer iff idle at/past the threshold AND not provably
// busy: a CPU above maxCPU spares it, but an unreadable CPU never disables the reap.
func reapVerdict(st engineerReapState, threshold time.Duration, maxCPU float64) (stop bool, reason string) {
	if !st.HasIdle {
		return false, "idle unknown (no timestamped log line and no start time); leaving it"
	}
	if st.Idle < threshold {
		return false, fmt.Sprintf("active %s ago (< %s idle threshold)", roundIdle(st.Idle), threshold)
	}
	if st.HasCPU && st.CPU > maxCPU {
		return false, fmt.Sprintf("idle %s but %.1f%% CPU (> %.1f%% guard): likely a live build/test, sparing it",
			roundIdle(st.Idle), st.CPU, maxCPU)
	}
	return true, fmt.Sprintf("idle %s (>= %s)%s", roundIdle(st.Idle), threshold, cpuNote(st))
}

// cpuNote annotates a stop reason with the CPU reading that cleared the guard, or
// notes the guard was skipped when CPU was unreadable.
func cpuNote(st engineerReapState) string {
	if !st.HasCPU {
		return ", CPU unreadable (guard skipped)"
	}
	return fmt.Sprintf(", %.1f%% CPU (idle)", st.CPU)
}

// roundIdle renders an idle duration to whole seconds so the report reads cleanly.
func roundIdle(d time.Duration) time.Duration {
	return d.Round(time.Second)
}
