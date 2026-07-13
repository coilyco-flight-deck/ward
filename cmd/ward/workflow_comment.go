package main

import "strings"

const (
	wardedWorkflowMarker = "WARDED_WORKFLOW:"

	legacyWardOutcomeMarker     = "WARD-OUTCOME:"
	legacyWardReservationMarker = "WARD-RESERVATION:"
	legacyWardDispatchMarker    = "WARD-DISPATCH:"
	legacyWardQaMarker          = "WARD-QA:"
	legacyWardStatusMarker      = "WARD-STATUS:"
	legacyWardReapMarker        = "WARD-REAP:"
	legacyWardTriageMarker      = "WARD-TRIAGE:"
)

var wardedWorkflowCommentVariants = []string{
	"reservation-held",
	"reservation-released",
	"dispatch-failed",
	"dispatch-deferred",
	"done",
	"submitted",
	"merge-ready",
	"blocked",
	"failed",
	"review-pass",
	"review-block",
	"review-advisory",
	"qa-pass",
	"qa-failed",
	"qa-blocked",
	"routed",
	"route-unclear",
	"pre-flight-no-go",
	"pre-flight-wrong-repo",
	"reopened",
	"triage",
}

var workflowCommentLegacyNonOutcomeVariants = map[string]struct{}{
	"qa-pass":               {},
	"qa-failed":             {},
	"qa-blocked":            {},
	"review-pass":           {},
	"review-block":          {},
	"review-advisory":       {},
	"status-done":           {},
	"status-submitted":      {},
	"status-merge-ready":    {},
	"status-blocked":        {},
	"status-failed":         {},
	"reap-done":             {},
	"reap-submitted":        {},
	"reap-merge-ready":      {},
	"reap-blocked":          {},
	"reap-failed":           {},
	"reservation-held":      {},
	"reservation-released":  {},
	"dispatch-failed":       {},
	"dispatch-deferred":     {},
	"pre-flight-no-go":      {},
	"pre-flight-wrong-repo": {},
	"routed":                {},
	"route-unclear":         {},
	"reopened":              {},
	"triage":                {},
}

type workflowCommentHeader struct {
	Variant string
	Detail  string
	Raw     string
	Legacy  bool
}

type workflowCommentHeaderParser func(string) (workflowCommentHeader, bool)

var workflowCommentHeaderParsers = []workflowCommentHeaderParser{
	parseWardedWorkflowCommentHeader,
	parseLegacyOutcomeWorkflowCommentHeader,
	parseLegacyReservationWorkflowCommentHeader,
	parseLegacyDispatchWorkflowCommentHeader,
	parseLegacyQaWorkflowCommentHeader,
	parseLegacyStatusWorkflowCommentHeader,
	parseLegacyReapWorkflowCommentHeader,
	parseLegacyTriageWorkflowCommentHeader,
}

func workflowCommentVisible(variant string, detail ...string) string {
	visible := wardedWorkflowMarker + " " + strings.TrimSpace(variant)
	if len(detail) > 0 {
		if s := strings.TrimSpace(detail[0]); s != "" {
			visible += " " + s
		}
	}
	return strings.TrimSpace(visible)
}

func parseWardedWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), wardedWorkflowMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(wardedWorkflowMarker):])
	return parseWorkflowCommentRest(rest, false)
}

func parseLegacyOutcomeWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), legacyWardOutcomeMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(legacyWardOutcomeMarker):])
	return parseWorkflowCommentRest(rest, true)
}

func parseLegacyReservationWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), legacyWardReservationMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(legacyWardReservationMarker):])
	return workflowCommentHeader{Variant: "reservation-held", Detail: rest, Legacy: true}, true
}

func parseLegacyDispatchWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), legacyWardDispatchMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(legacyWardDispatchMarker):])
	variant := "dispatch"
	if rest != "" {
		if head, tail, ok := strings.Cut(rest, " "); ok {
			variant = strings.TrimSpace(head)
			rest = strings.TrimSpace(tail)
		}
	}
	return workflowCommentHeader{Variant: canonicalWorkflowCommentVariant(variant), Detail: rest, Legacy: true}, true
}

func parseLegacyQaWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), legacyWardQaMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(legacyWardQaMarker):])
	return parseWorkflowCommentRest("qa-"+rest, true)
}

func parseLegacyStatusWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), legacyWardStatusMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(legacyWardStatusMarker):])
	return parseWorkflowCommentRest("status-"+rest, true)
}

func parseLegacyReapWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), legacyWardReapMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(legacyWardReapMarker):])
	return parseWorkflowCommentRest("reap-"+rest, true)
}

func parseLegacyTriageWorkflowCommentHeader(s string) (workflowCommentHeader, bool) {
	if !strings.HasPrefix(strings.ToUpper(s), legacyWardTriageMarker) {
		return workflowCommentHeader{}, false
	}
	rest := strings.TrimSpace(s[len(legacyWardTriageMarker):])
	return workflowCommentHeader{Variant: "triage", Detail: rest, Legacy: true}, true
}

func workflowOutcomeVisible(status string) string {
	status = normalizeBacklogOutcomeStatus(status)
	return workflowCommentVisible(status, outcomeStatusEmoji(status))
}

func workflowReservationHeldVisible() string { return workflowCommentVisible("reservation-held") }
func workflowReservationReleasedVisible() string {
	return workflowCommentVisible("reservation-released")
}
func workflowDispatchFailedVisible() string   { return workflowCommentVisible("dispatch-failed") }
func workflowDispatchDeferredVisible() string { return workflowCommentVisible("dispatch-deferred") }
func workflowReviewVisible(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "pass", "passed":
		return workflowCommentVisible("review-pass")
	case "blocked":
		return workflowCommentVisible("review-block")
	case "advisory":
		return workflowCommentVisible("review-advisory")
	default:
		return workflowCommentVisible("review-" + strings.TrimSpace(status))
	}
}
func workflowQAVisible(status string, emoji string) string {
	return workflowCommentVisible("qa-"+strings.TrimSpace(status), emoji)
}
func workflowStatusVisible(status string, detail ...string) string {
	return workflowCommentVisible(strings.TrimSpace(status), detail...)
}
func workflowReapVisible(status string) string {
	return workflowCommentVisible(strings.TrimSpace(status))
}

func parseWorkflowCommentHeader(body string) (workflowCommentHeader, bool) {
	for _, ln := range strings.Split(body, "\n") {
		if header, ok := parseWorkflowCommentHeaderLine(ln); ok {
			return header, true
		}
	}
	return workflowCommentHeader{}, false
}

func parseWorkflowCommentHeaderLine(ln string) (workflowCommentHeader, bool) {
	s := workflowCommentLine(ln)
	if s == "" {
		return workflowCommentHeader{}, false
	}
	for _, parse := range workflowCommentHeaderParsers {
		if header, ok := parse(s); ok {
			return header, true
		}
	}
	return workflowCommentHeader{}, false
}

func parseWorkflowCommentRest(rest string, legacy bool) (workflowCommentHeader, bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return workflowCommentHeader{}, false
	}
	variant, detail, _ := strings.Cut(rest, " ")
	variant = canonicalWorkflowCommentVariant(variant)
	return workflowCommentHeader{Variant: variant, Detail: workflowCommentDetail(detail), Raw: rest, Legacy: legacy}, true
}

func canonicalWorkflowCommentVariant(s string) string {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "pending":
		return "submitted"
	case "ready-for-merge":
		return "merge-ready"
	case "pass":
		return "pass"
	case "fail":
		return "failed"
	default:
		return strings.TrimSpace(strings.ToLower(s))
	}
}

func workflowCommentLine(ln string) string {
	s := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), ">*-•# "))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(s), "<details") {
		return ""
	}
	if strings.HasPrefix(s, "<!--") {
		return ""
	}
	return s
}

func workflowCommentDetail(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "-:.\u2022✅🛑❌ ")
	return strings.TrimSpace(s)
}

func workflowCommentIsTerminalOutcomeVariant(variant string) bool {
	switch normalizeBacklogOutcomeStatus(variant) {
	case "done", "submitted", "merge-ready", "blocked", "failed":
		return true
	default:
		return false
	}
}

func workflowCommentIsLegacyWorkflowCommentVariant(variant string) bool {
	_, ok := workflowCommentLegacyNonOutcomeVariants[strings.TrimSpace(strings.ToLower(variant))]
	return ok
}

func workflowCommentSummary(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for _, ln := range lines {
		s := workflowCommentLine(ln)
		if s == "" {
			continue
		}
		if _, ok := parseWorkflowCommentHeader(s); ok {
			continue
		}
		return s
	}
	return ""
}
