package main

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	backlogKindIssue       = "issue"
	backlogKindPullRequest = "pull-request"
)

var (
	backlogTierOrder = []string{"P0", "P1", "P2", "P3", "P4"}
	backlogOutcomeRE = regexp.MustCompile(`(?i)^(done|submitted|merge-ready|pending|ready-for-merge|blocked|failed)\b(?:\s+[✅🛑❌])?[\s:.\-]*(.*)`)
)

// backlogIssue is the forge-neutral row used by the retained live queue reader.
type backlogIssue struct {
	Number int
	Kind   string
	Author string
	Title  string
	Body   string
	Labels []string
	URL    string
}

func backlogTierOf(labels []string) string {
	for _, tier := range backlogTierOrder {
		for _, label := range labels {
			if label == tier {
				return tier
			}
		}
	}
	return ""
}

func backlogKindOf(kind string) string {
	if strings.EqualFold(strings.TrimSpace(kind), backlogKindPullRequest) {
		return backlogKindPullRequest
	}
	return backlogKindIssue
}

func parseBacklogOutcome(comments []issueComment) *backlogOutcome {
	latest, ok := latestBacklogOutcomeComment(comments)
	if !ok {
		return nil
	}
	outcome, ok := backlogOutcomeOfComment(latest.Body)
	if !ok {
		return nil
	}
	return &outcome
}

func latestBacklogOutcomeComment(comments []issueComment) (issueComment, bool) {
	if humanFeedbackOutcomeBlocked(comments, time.Time{}) {
		return issueComment{}, false
	}
	type hit struct {
		at time.Time
		c  issueComment
	}
	var hits []hit
	for _, comment := range comments {
		if !trustedMachineComment(comment, recordKindOutcome) {
			continue
		}
		if _, ok := backlogOutcomeOfComment(comment.Body); !ok {
			continue
		}
		hits = append(hits, hit{at: comment.CreatedAt, c: comment})
	}
	if len(hits) == 0 {
		return issueComment{}, false
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at.Before(hits[j].at) })
	return hits[len(hits)-1].c, true
}

type directorRunMeta struct {
	Workflow           string
	Review             string
	MergeAuthorization string
	Outcome            backlogOutcome
	HasOutcome         bool
	IssueRef           string
	QA                 qaCommentMeta
	PRHeadSHA          string
	PRRef              string
	Status             directorMergeStatusSummary
	CommentedBy        string
	CommentedAt        time.Time
}

func latestQAVerdictComment(comments []issueComment, issueRef, prRef, headSHA string) (qaCommentMeta, bool) {
	type hit struct {
		at time.Time
		m  qaCommentMeta
	}
	var hits []hit
	issueRef = strings.TrimSpace(issueRef)
	prRef = strings.TrimSpace(prRef)
	headSHA = strings.TrimSpace(headSHA)
	if issueRef == "" || prRef == "" || headSHA == "" {
		return qaCommentMeta{}, false
	}
	for _, comment := range comments {
		if !trustedMachineComment(comment, recordKindQA) {
			continue
		}
		meta, ok := parseQAVerdictComment(comment.Body)
		if !ok || strings.TrimSpace(meta.IssueRef) != issueRef || strings.TrimSpace(meta.PRRef) != prRef || strings.TrimSpace(meta.ReviewedSHA) != headSHA {
			continue
		}
		hits = append(hits, hit{at: comment.CreatedAt, m: meta})
	}
	if len(hits) == 0 {
		return qaCommentMeta{}, false
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at.Before(hits[j].at) })
	return hits[len(hits)-1].m, true
}

func parseDirectorRunMeta(body string) directorRunMeta {
	meta := directorRunMeta{}
	if outcome, ok := backlogOutcomeOfComment(body); ok {
		meta.Outcome = outcome
		meta.HasOutcome = true
	}
	for _, line := range strings.Split(body, "\n") {
		visible := backlogCommentLine(line)
		for _, part := range strings.Split(visible, ";") {
			field := strings.TrimSpace(part)
			lower := strings.ToLower(field)
			switch {
			case strings.HasPrefix(lower, "workflow:"):
				meta.Workflow = string(canonicalWorkflow(workflowMode(strings.TrimSpace(field[len("workflow:"):]))))
			case strings.HasPrefix(lower, "review summary:"):
				meta.Review = strings.TrimSpace(field[len("review summary:"):])
			case strings.HasPrefix(lower, "director merge authorization:"):
				meta.MergeAuthorization = strings.TrimSpace(field[len("director merge authorization:"):])
			case strings.HasPrefix(lower, "checked head sha:"):
				meta.Status.HeadSHA = strings.TrimSpace(field[len("checked head sha:"):])
			case strings.HasPrefix(lower, "status state:"):
				meta.Status.State = strings.TrimSpace(field[len("status state:"):])
			case strings.HasPrefix(lower, "status context:"):
				meta.Status.Checks = parseDirectorStatusContexts(strings.TrimSpace(field[len("status context:"):]))
			}
		}
	}
	return meta
}

func parseDirectorStatusContexts(value string) []directorMergeStatusContext {
	value = strings.TrimSpace(value)
	if value == "" || value == "<status unavailable>" {
		return nil
	}
	var out []directorMergeStatusContext
	for _, part := range strings.Split(value, ",") {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		contextName, state, ok := strings.Cut(field, "=")
		if !ok {
			out = append(out, directorMergeStatusContext{Context: field})
			continue
		}
		out = append(out, directorMergeStatusContext{Context: strings.TrimSpace(contextName), State: strings.TrimSpace(state)})
	}
	return out
}

func backlogCommentLine(line string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), ">*-•# "))
}

func backlogOutcomeOfComment(body string) (backlogOutcome, bool) {
	header, ok := parseWorkflowCommentHeader(body)
	if !ok {
		return backlogOutcome{}, false
	}
	if outcome, ok := backlogOutcomeFromPRURLHeader(body, header); ok {
		return outcome, true
	}
	outcome := backlogOutcome{Status: "unknown"}
	if strings.Contains(strings.TrimSpace(header.Variant), "://") {
		return backlogOutcome{}, false
	}
	status := normalizeBacklogOutcomeStatus(header.Variant)
	switch {
	case workflowCommentIsTerminalOutcomeVariant(header.Variant):
		outcome.Status = status
		outcome.Text = header.Detail
	case !header.Legacy || workflowCommentIsLegacyWorkflowCommentVariant(header.Variant):
		return backlogOutcome{}, false
	default:
		outcome.Text = workflowCommentDetail(header.Raw)
	}
	if match := backlogOutcomeRE.FindStringSubmatch(header.Detail); match != nil {
		outcome.Status = normalizeBacklogOutcomeStatus(strings.ToLower(match[1]))
		outcome.Text = workflowCommentDetail(match[2])
	} else if outcome.Status != "unknown" {
		outcome.Text = workflowCommentDetail(outcome.Text)
	}
	outcome.Text = backlogTruncate(outcome.Text, 500)
	return outcome, true
}

func backlogOutcomeFromPRURLHeader(body string, header workflowCommentHeader) (backlogOutcome, bool) {
	pr, ok := parseWorkflowOutcomePRRef(header.Variant)
	if !ok || strings.TrimSpace(header.Detail) != "" {
		return backlogOutcome{}, false
	}
	outcome := backlogOutcome{Status: "submitted", Text: workflowCommentDetail(pr.url()), PRURL: pr.url(), PRNumber: pr.Number}
	if auth, ok := workflowCommentFieldValue(body, "director merge authorization:"); ok {
		switch strings.ToLower(strings.TrimSpace(auth)) {
		case "reviewed-and-ready", "merge-ready":
			outcome.Status = "merge-ready"
		}
	}
	return outcome, true
}

func normalizeBacklogOutcomeStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "pending":
		return "submitted"
	case "ready-for-merge":
		return "merge-ready"
	default:
		return strings.TrimSpace(strings.ToLower(status))
	}
}

func backlogOutcomeState(status string) string {
	switch normalizeBacklogOutcomeStatus(status) {
	case "done":
		return "done"
	case "failed":
		return "failed"
	case "blocked":
		return "blocked"
	case "submitted":
		return "submitted"
	case "merge-ready":
		return "merge-ready"
	default:
		return "blocked"
	}
}

func backlogEntryKindPrefix(kind string) string {
	if backlogKindOf(kind) == backlogKindPullRequest {
		return "PR"
	}
	return "ISSUE"
}

func tierOrDash(tier string) string {
	if strings.TrimSpace(tier) == "" {
		return "--"
	}
	return tier
}

func backlogTruncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	return strings.TrimSpace(value[:limit-1]) + "…"
}
