package main

import (
	"fmt"
	"strings"
	"time"
)

// qaThoroughness is one rung of the QA depth ladder: how hard the inspection
// digs, the wall-clock it gets, and the steer woven into its prompt.
type qaThoroughness struct {
	Name     string
	Timeout  time.Duration
	Guidance string
}

var qaThoroughnessLevels = []qaThoroughness{
	{
		Name:    "quick",
		Timeout: 3 * time.Minute,
		Guidance: "Keep this QUICK: inspect the obvious candidate state, issue thread, and checks. " +
			"Prefer a compact verdict over exhaustive exploration.",
	},
	{
		Name:    "standard",
		Timeout: 8 * time.Minute,
		Guidance: "Inspect at a STANDARD depth: review the candidate branch or PR, the issue thread, " +
			"and the available checks, then give a well-structured verdict with evidence.",
	},
	{
		Name:    "deep",
		Timeout: 15 * time.Minute,
		Guidance: "Go DEEP: inspect thoroughly, chase edge cases, compare the implementation to the " +
			"issue contract, and cite the concrete evidence behind the verdict.",
	},
}

const (
	defaultQAThoroughness        = "standard"
	containerInspectionSetupTime = 5 * time.Minute
)

// parseQAThoroughness resolves a --thoroughness value to a known depth.
func parseQAThoroughness(s string) (qaThoroughness, error) {
	want := strings.ToLower(strings.TrimSpace(s))
	if want == "" {
		want = defaultQAThoroughness
	}
	for _, lvl := range qaThoroughnessLevels {
		if lvl.Name == want {
			return lvl, nil
		}
	}
	names := make([]string, 0, len(qaThoroughnessLevels))
	for _, lvl := range qaThoroughnessLevels {
		names = append(names, lvl.Name)
	}
	return qaThoroughness{}, fmt.Errorf("unknown --thoroughness %q: want %s", s, strings.Join(names, "|"))
}

// extractJSONBlock pulls the JSON object from a read: it anchors on a ```json fence
// when present, then brace-matches to the object's true end.
func extractJSONBlock(read string) (string, bool) {
	start := 0
	if i := strings.Index(read, "```json"); i >= 0 {
		start = i + len("```json")
	}
	if obj, ok := scanJSONObject(read[start:]); ok {
		return obj, true
	}
	if i := strings.Index(read, "{"); i >= 0 {
		if j := strings.LastIndex(read, "}"); j > i {
			return strings.TrimSpace(read[i : j+1]), true
		}
	}
	return "", false
}

func scanJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(s[start : i+1]), true
			}
		}
	}
	return "", false
}
