package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
)

type issueCommentPoster interface {
	CommentIssue(context.Context, string, string, int, string) error
}

type humanFeedbackRules struct {
	ignoredAuthors    map[string]struct{}
	automationMarkers []string
}

func loadHumanFeedbackRules() humanFeedbackRules {
	rules := humanFeedbackRules{}
	path, err := config.GlobalConfigPath()
	if err != nil {
		return rules
	}
	var cfg wardGlobalConfig
	if oerr := config.OverlayFile(&cfg, path); oerr != nil {
		return rules
	}
	for _, raw := range cfg.Agent.HumanFeedback.IgnoreAuthors {
		if login := strings.ToLower(strings.TrimSpace(raw)); login != "" {
			if rules.ignoredAuthors == nil {
				rules.ignoredAuthors = make(map[string]struct{})
			}
			rules.ignoredAuthors[login] = struct{}{}
		}
	}
	for _, raw := range cfg.Agent.HumanFeedback.AutomationMarkers {
		if marker := strings.TrimSpace(raw); marker != "" {
			rules.automationMarkers = append(rules.automationMarkers, marker)
		}
	}
	return rules
}

func (r humanFeedbackRules) ignoresAuthor(login string) bool {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" || len(r.ignoredAuthors) == 0 {
		return false
	}
	_, ok := r.ignoredAuthors[login]
	return ok
}

func (r humanFeedbackRules) wardAuthoredComment(c issueComment) bool {
	if isWardAutomationComment(c.Body, r.automationMarkers) {
		return true
	}
	return r.ignoresAuthor(c.User.Login)
}

func isWardAutomationComment(body string, extraMarkers []string) bool {
	if _, ok := parseWorkflowCommentHeader(body); ok {
		return true
	}
	for _, raw := range extraMarkers {
		marker := strings.ToLower(strings.TrimSpace(raw))
		if marker == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), marker) {
			return true
		}
	}
	return false
}

func wardAuthoredComment(c issueComment) bool {
	return loadHumanFeedbackRules().wardAuthoredComment(c)
}

func latestWardAndHumanCommentsWithRules(comments []issueComment, rules humanFeedbackRules) (ward *issueComment, human *issueComment) {
	for i := range comments {
		c := &comments[i]
		if rules.wardAuthoredComment(*c) {
			if ward == nil || c.CreatedAt.After(ward.CreatedAt) {
				ward = c
			}
			continue
		}
		if strings.TrimSpace(c.Body) == "" {
			continue
		}
		if human == nil || c.CreatedAt.After(human.CreatedAt) {
			human = c
		}
	}
	return ward, human
}

func humanInterventionBlockReason(comments []issueComment, snapshot time.Time) (string, bool) {
	return humanInterventionBlockReasonWithRules(comments, snapshot, loadHumanFeedbackRules())
}

func humanInterventionBlockReasonWithRules(comments []issueComment, snapshot time.Time, rules humanFeedbackRules) (string, bool) {
	ward, human := latestWardAndHumanCommentsWithRules(comments, rules)
	wardAt := time.Time{}
	if ward != nil {
		wardAt = ward.CreatedAt
	}
	if human != nil && (ward == nil || !human.CreatedAt.Before(wardAt)) {
		if ward == nil {
			return fmt.Sprintf("human comment by @%s at %s has no newer ward acknowledgement yet",
				strings.TrimSpace(human.User.Login), human.CreatedAt.UTC().Format(time.RFC3339)), true
		}
		return fmt.Sprintf("human comment by @%s at %s is newer than the latest ward acknowledgement at %s",
			strings.TrimSpace(human.User.Login), human.CreatedAt.UTC().Format(time.RFC3339), wardAt.UTC().Format(time.RFC3339)), true
	}
	if snapshot.IsZero() {
		return "", false
	}
	if ward == nil {
		return fmt.Sprintf("manual close/update snapshot at %s has no newer ward acknowledgement yet",
			snapshot.UTC().Format(time.RFC3339)), true
	}
	if snapshot.After(wardAt) {
		return fmt.Sprintf("manual close/update snapshot at %s is newer than the latest ward acknowledgement at %s",
			snapshot.UTC().Format(time.RFC3339), wardAt.UTC().Format(time.RFC3339)), true
	}
	return "", false
}

func humanInterventionBlockComment(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "human feedback requires a visible acknowledgement before merge or reopen."
	}
	return collapsedIssueComment(workflowOutcomeVisible("blocked"), "details", reason+"\n\nThis action is blocked until the feedback is visibly acknowledged.")
}

func reportHumanInterventionBlock(ctx context.Context, owner, repo string, issueNum, prNum int, reason string, issuePoster, prPoster issueCommentPoster) {
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "human comment") && !strings.Contains(lower, "manual close/update snapshot") {
		return
	}
	body := humanInterventionBlockComment(reason)
	if issuePoster != nil && issueNum > 0 {
		if err := issuePoster.CommentIssue(ctx, owner, repo, issueNum, body); err != nil {
			fmt.Fprintf(os.Stderr, "ward human-feedback block: could not post on %s/%s#%d: %v\n", owner, repo, issueNum, err)
		}
	}
	if prPoster != nil && prNum > 0 {
		if err := prPoster.CommentIssue(ctx, owner, repo, prNum, body); err != nil {
			fmt.Fprintf(os.Stderr, "ward human-feedback block: could not mirror on %s/%s#%d: %v\n", owner, repo, prNum, err)
		}
	}
}

func prWorkflowHumanInterventionGuard(ctx context.Context, cl *forgejoClient, owner, repo string, index int, snapshot time.Time, label string) error {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return fmt.Errorf("%s: read pull request: %w", label, err)
	}
	comments, err := cl.ListIssueComments(ctx, owner, repo, index)
	if err != nil {
		return fmt.Errorf("%s: read comment thread: %w", label, err)
	}
	snapshot = laterTime(snapshot, pr.UpdatedAt)
	issueNum := index
	if linked, ok := directorLinkedIssueNumber(pr.Body); ok {
		linkedComments, lerr := cl.ListIssueComments(ctx, owner, repo, linked)
		if lerr != nil {
			return fmt.Errorf("%s: read linked issue comments: %w", label, lerr)
		}
		comments = append(append([]issueComment(nil), linkedComments...), comments...)
		if issue, ierr := cl.GetIssue(ctx, owner, repo, linked); ierr == nil {
			snapshot = laterTime(snapshot, issue.UpdatedAt)
		}
		issueNum = linked
	}
	if reason, blocked := humanInterventionBlockReason(comments, snapshot); blocked {
		reportHumanInterventionBlock(ctx, owner, repo, issueNum, index, reason, cl, cl)
		return fmt.Errorf("%s: %s", label, reason)
	}
	return nil
}

func humanFeedbackOutcomeBlocked(comments []issueComment, snapshot time.Time) bool {
	_, blocked := humanInterventionBlockReason(comments, snapshot)
	return blocked
}
