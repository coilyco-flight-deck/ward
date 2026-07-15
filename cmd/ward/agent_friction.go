package main

import "strings"

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

	if !meta.Launched {
		events = append(events, frictionEvent{
			Stage:       "reap",
			Category:    "prelaunch-failure",
			Severity:    "high",
			Confidence:  "high",
			Fingerprint: "reap/prelaunch/container-never-launched",
			Evidence:    frictionEvidence(console, "pre-launch", "did no work", "launch failed"),
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

func frictionEvidence(console string, markers ...string) string {
	if line := consoleLine(console, markers...); line != "" {
		return line
	}
	return "agent never reached launch"
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
