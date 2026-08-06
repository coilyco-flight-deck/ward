// Package reviewpanel is the pure core of ward's in-container adversarial-review
// gate (ward#134): the diff a worker produced must survive a heterogeneous
// multi-model review panel before the run is allowed to land (open the PR /
// merge). Everything here is deterministic and free of subprocess, network, and
// filesystem side effects so the gate logic is exhaustively unit-testable; the
// cmd/ward side (agent_review.go) wires the fleet roster, the reviewer
// subprocesses, and the persisted sidecar log onto it. See docs/agent-workflow.md.
package reviewpanel

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Verdict is a reviewer's binary call on a diff: clear or block, no soft middle a
// rubber stamp could hide in.
type Verdict string

const (
	// Pass clears the diff: the reviewer tried to refute it and could not.
	Pass Verdict = "pass"
	// Block rejects the diff, and is the fail-closed default for any unparseable,
	// errored, or timed-out reviewer (a non-answer is never a pass; ward#134).
	Block Verdict = "block"
)

// Class is the autonomy/risk class that tiers the quorum threshold (ward#134),
// designed to later key off the triage filter's per-issue label. docs/agent-workflow.md.
type Class string

const (
	// ClassLintCleanup is the lowest-risk tier: clears on a single passing reviewer.
	ClassLintCleanup Class = "lint-cleanup"
	// ClassDefault is the middle tier every unclassified diff takes: a majority passes.
	ClassDefault Class = "default"
	// ClassRefactor is the highest-risk tier: needs the whole panel unanimous.
	ClassRefactor Class = "refactor"
)

// ParseClass resolves a class string (empty = default), erroring on an unknown
// value so a typo fails loud instead of picking a laxer tier.
func ParseClass(s string) (Class, error) {
	switch Class(strings.TrimSpace(s)) {
	case "", ClassDefault:
		return ClassDefault, nil
	case ClassLintCleanup:
		return ClassLintCleanup, nil
	case ClassRefactor:
		return ClassRefactor, nil
	default:
		return "", fmt.Errorf("invalid review class %q: want %s|%s|%s",
			s, ClassLintCleanup, ClassDefault, ClassRefactor)
	}
}

// RequiredPasses is the PASS count the class needs given n binding reviewers: lint 1,
// refactor n, default a majority. n<=0 returns 0 (an empty panel never passes).
func (c Class) RequiredPasses(n int) int {
	if n <= 0 {
		return 0
	}
	switch c {
	case ClassLintCleanup:
		return 1
	case ClassRefactor:
		return n
	case ClassDefault:
		return n/2 + 1
	default:
		return n/2 + 1
	}
}

// ReviewerResult is one reviewer's structured verdict. Advisory marks a non-binding
// reviewer (fallback); a non-empty Error forces a fail-closed Block.
type ReviewerResult struct {
	Family     string  `json:"family"`
	Model      string  `json:"model,omitempty"`
	Verdict    Verdict `json:"verdict"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	Advisory   bool    `json:"advisory,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// counts reports whether this reviewer's verdict counts toward quorum: a binding,
// non-errored PASS. Everything else does not - the fail-closed core of the gate.
func (r ReviewerResult) counts() bool {
	return !r.Advisory && r.Error == "" && r.Verdict == Pass
}

// Gate is the panel's overall decision; Advisory is the non-binding single-family
// fallback (the panel could not be made heterogeneous).
type Gate string

const (
	// GatePass clears the run to land: quorum was met.
	GatePass Gate = "pass"
	// GateBlock stops the run from landing: quorum unmet, or a reviewer failed closed.
	GateBlock Gate = "block"
	// GateAdvisory is the degraded, non-binding outcome: no heterogeneous reviewer was
	// available, so the panel advises loudly but does not block.
	GateAdvisory Gate = "advisory"
)

// Evaluate computes the gate from the reviewer results and class, fail-closed: only a
// binding non-errored PASS counts, a shortfall blocks, zero binding reviewers advisory.
func Evaluate(class Class, results []ReviewerResult) (gate Gate, passes, threshold int) {
	binding := 0
	for _, r := range results {
		if !r.Advisory {
			binding++
		}
		if r.counts() {
			passes++
		}
	}
	if binding == 0 {
		return GateAdvisory, 0, 0
	}
	threshold = class.RequiredPasses(binding)
	if passes >= threshold {
		return GatePass, passes, threshold
	}
	return GateBlock, passes, threshold
}

// PanelResult is the persisted outcome of one panel run - one JSONL row in the sidecar
// log beside the audit trail, enough to compute the measurement rates (ward#134).
type PanelResult struct {
	Timestamp int64            `json:"ts"`
	Issue     string           `json:"issue,omitempty"`
	Worker    string           `json:"worker"`
	Class     Class            `json:"class"`
	Gate      Gate             `json:"gate"`
	Passes    int              `json:"passes"`
	Threshold int              `json:"threshold"`
	Reviewers []ReviewerResult `json:"reviewers"`
	Note      string           `json:"note,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
}

// Blocks reports whether this result stops the run from landing: only a hard
// GateBlock does, never advisory.
func (p PanelResult) Blocks() bool { return p.Gate == GateBlock }

// verdictBlock matches a fenced ```json block a reviewer emits; the last wins, and
// the fence tag is case-insensitive.
var verdictBlock = regexp.MustCompile("(?is)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// rawVerdict is the wire shape a reviewer emits: a verdict, a reason, and a
// confidence in [0,1]. Parsed leniently, then validated fail-closed.
type rawVerdict struct {
	Verdict    string  `json:"verdict"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// ParseVerdict extracts a structured verdict from a reviewer's stdout, fail-closed:
// empty, unparseable, or unrecognized output becomes a Block, never a silent pass.
func ParseVerdict(family, model, stdout string) ReviewerResult {
	base := ReviewerResult{Family: family, Model: model, Verdict: Block}
	raw, ok := extractVerdict(stdout)
	if !ok {
		base.Error = "no parseable {verdict,...} json block in reviewer output"
		base.Reason = "reviewer produced no structured verdict; blocking fail-closed"
		return base
	}
	base.Confidence = clampConfidence(raw.Confidence)
	base.Reason = strings.TrimSpace(raw.Reason)
	switch Verdict(strings.ToLower(strings.TrimSpace(raw.Verdict))) {
	case Pass:
		base.Verdict = Pass
	case Block:
		base.Verdict = Block
	default:
		base.Error = fmt.Sprintf("unrecognized verdict %q", raw.Verdict)
		base.Verdict = Block
		if base.Reason == "" {
			base.Reason = "reviewer returned an unrecognized verdict; blocking fail-closed"
		}
	}
	return base
}

// extractVerdict pulls the winning JSON object out of stdout: the last fenced
// json block if present, else a bare trailing object, else nothing.
func extractVerdict(stdout string) (rawVerdict, bool) {
	var out rawVerdict
	if m := verdictBlock.FindAllStringSubmatch(stdout, -1); len(m) > 0 {
		if err := json.Unmarshal([]byte(m[len(m)-1][1]), &out); err == nil {
			return out, true
		}
	}
	// Fall back to the last bare {...} object on the tail, for a reviewer that
	// emitted valid JSON without a fence.
	if obj, ok := lastBraceObject(stdout); ok {
		if err := json.Unmarshal([]byte(obj), &out); err == nil {
			return out, true
		}
	}
	return rawVerdict{}, false
}

// lastBraceObject returns the last balanced top-level {...} span in s, ignoring braces
// inside strings so a reason containing "{" does not derail it.
func lastBraceObject(s string) (string, bool) {
	start, depth, inStr, esc := -1, 0, false, false
	best := ""
	for i, r := range s {
		if inStr {
			inStr, esc = stepInString(r, esc)
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
			if depth == 0 && start >= 0 {
				best = s[start : i+1]
			}
		}
	}
	return best, best != ""
}

// stepInString advances the string-literal scan by one rune, returning the next
// (inStr, esc) state: an unescaped quote closes the string, a backslash escapes.
func stepInString(r rune, esc bool) (inStr, nextEsc bool) {
	switch {
	case esc:
		return true, false
	case r == '\\':
		return true, true
	case r == '"':
		return false, false
	default:
		return true, false
	}
}

// clampConfidence pins a reviewer's confidence into [0,1]; NaN/absent reads as 0.
func clampConfidence(c float64) float64 {
	if math.IsNaN(c) {
		return 0
	}
	return math.Max(0, math.Min(1, c))
}
