package main

import (
	"strconv"
	"strings"
)

// frictionEvent is one deterministic per-run friction fact emitted alongside
// the drained meta.json summary.
type frictionEvent struct {
	Stage       string `json:"stage"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Confidence  string `json:"confidence"`
	Fingerprint string `json:"fingerprint"`
	Evidence    string `json:"evidence"`
}

func collectFrictionEvents(meta runMeta, console string) []frictionEvent {
	var events []frictionEvent

	if transcriptExpected(meta.Driver) && !meta.TranscriptPresent {
		events = append(events, frictionEvent{
			Stage:       "drain",
			Category:    "missing-transcript",
			Severity:    "medium",
			Confidence:  "high",
			Fingerprint: "drain/transcript/missing/" + frictionSlug(meta.Driver),
			Evidence:    "drain archive had no transcript.jsonl",
		})
	}

	if !meta.Launched && !meta.TranscriptPresent {
		events = append(events, frictionEvent{
			Stage:       "reap",
			Category:    "prelaunch-failure",
			Severity:    "high",
			Confidence:  "high",
			Fingerprint: "reap/prelaunch/container-never-launched",
			Evidence:    frictionLifecycleEvidence(console, "pre-launch", "did no work", "launch failed"),
		})
	}

	if line := consoleLine(console, "preserved un-landed granted-repo work"); line != "" {
		events = append(events, frictionEvent{
			Stage:       "reap",
			Category:    "extra-repo-preservation",
			Severity:    "medium",
			Confidence:  "high",
			Fingerprint: "reap/preserved-extra-repo/" + frictionSlug(extraRepoFingerprint(line)),
			Evidence:    line,
		})
	}

	if line := consoleLine(console, "preserved work on"); line != "" {
		events = append(events, frictionEvent{
			Stage:       "reap",
			Category:    "salvage-noise",
			Severity:    "medium",
			Confidence:  "high",
			Fingerprint: "reap/preserved-branch/" + frictionSlug(salvageFingerprint(line)),
			Evidence:    line,
		})
	}

	events = append(events, collectBrokerFrictionEvents(console, dispatchArtifactMeta{})...)
	return events
}

func collectDispatchFrictionEvents(meta dispatchArtifactMeta, console string) []frictionEvent {
	return collectBrokerFrictionEvents(console, meta)
}

func collectBrokerFrictionEvents(console string, meta dispatchArtifactMeta) []frictionEvent {
	var events []frictionEvent
	if count := countConsoleLines(console, "reference is not a tree"); count > 0 {
		events = append(events, frictionEvent{
			Stage:       "broker",
			Category:    "stale-config-ref",
			Severity:    "medium",
			Confidence:  "high",
			Fingerprint: "broker/config-ref/reference-not-tree",
			Evidence:    countEvidence("reference is not a tree", count),
		})
	}
	if count := countGeneratedMountDegradation(console); count > 0 {
		events = append(events, frictionEvent{
			Stage:       "broker",
			Category:    "generated-mount-degradation",
			Severity:    "medium",
			Confidence:  "high",
			Fingerprint: "broker/generated-mount/degraded",
			Evidence:    countEvidence("generated mount degraded", count),
		})
	}
	if line := consoleLine(console, "image pull failed", "trying the local image"); line != "" {
		events = append(events, frictionEvent{
			Stage:       "broker",
			Category:    "image-pull-fallback",
			Severity:    "low",
			Confidence:  "high",
			Fingerprint: "broker/image-pull/local-fallback",
			Evidence:    line,
		})
	}
	if dispatchArtifactTerminalFailure(meta) {
		evidence := strings.TrimSpace(meta.Error)
		if evidence == "" {
			evidence = "dispatch outcome " + strings.TrimSpace(meta.Outcome)
		}
		events = append(events, frictionEvent{
			Stage:       "broker",
			Category:    "terminal-launch-failure",
			Severity:    "high",
			Confidence:  "high",
			Fingerprint: "broker/launch/terminal-" + frictionSlug(firstNonEmpty(meta.ErrorClass, meta.Outcome)),
			Evidence:    evidence,
		})
	}
	return events
}

func transcriptExpected(driver string) bool {
	switch containerMode(strings.TrimSpace(driver)) {
	case modeClaude, modeCodex:
		return true
	case modeOpencode, modeGoose:
		return false
	default:
		return false
	}
}

func consoleLine(console string, markers ...string) string {
	for _, raw := range strings.Split(console, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(strings.ToLower(line), strings.ToLower(marker)) {
				return line
			}
		}
	}
	return ""
}

func consoleLifecycleLine(console string, markers ...string) string {
	for _, raw := range strings.Split(console, "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		if line == "" || (!strings.HasPrefix(line, "WARD-REAP:") && !strings.HasPrefix(lower, "ward container reap:")) {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(lower, strings.ToLower(marker)) {
				return line
			}
		}
	}
	return ""
}

func frictionLifecycleEvidence(console string, markers ...string) string {
	if line := consoleLifecycleLine(console, markers...); line != "" {
		return line
	}
	return "agent never reached launch"
}

func countConsoleLines(console string, marker string) int {
	count := 0
	needle := strings.ToLower(marker)
	for _, raw := range strings.Split(console, "\n") {
		if strings.Contains(strings.ToLower(raw), needle) {
			count++
		}
	}
	return count
}

func countGeneratedMountDegradation(console string) int {
	count := 0
	for _, raw := range strings.Split(console, "\n") {
		line := strings.ToLower(raw)
		if strings.Contains(line, "generated") && strings.Contains(line, "mount") &&
			(strings.Contains(line, "degrad") || strings.Contains(line, "fallback")) {
			count++
		}
	}
	return count
}

func countEvidence(label string, count int) string {
	if count == 1 {
		return label + " (1 occurrence)"
	}
	return label + " (" + strconv.Itoa(count) + " occurrences)"
}

func dispatchArtifactTerminalFailure(meta dispatchArtifactMeta) bool {
	outcome := strings.TrimSpace(meta.Outcome)
	if outcome == "" || outcome == "in-progress" || outcome == "launched" {
		return false
	}
	switch outcome {
	case "deferred-open-pr", "deferred-release-assets-not-ready", "deferred-reservation-conflict", "deferred-capacity", "partial-launch":
		return false
	default:
		return true
	}
}

func extraRepoFingerprint(line string) string {
	if i := strings.LastIndex(line, "("); i >= 0 {
		if j := strings.LastIndex(line, ")"); j > i {
			return strings.TrimSpace(line[i+1 : j])
		}
	}
	return line
}

func salvageFingerprint(line string) string {
	if i := strings.Index(line, "("); i >= 0 {
		if j := strings.LastIndex(line, ")"); j > i {
			return strings.TrimSpace(line[i+1 : j])
		}
	}
	return line
}

func frictionSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
