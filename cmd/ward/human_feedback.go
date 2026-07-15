package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

type issueCommentPoster interface {
	CommentIssue(context.Context, string, string, int, string) error
}

func wardAuthoredComment(c issueComment) bool {
	if strings.EqualFold(strings.TrimSpace(c.User.Login), forgeForgejo.gitPushUser()) {
		return true
	}
	_, ok := parseWorkflowCommentHeader(c.Body)
	return ok
}

func latestWardAndHumanComments(comments []issueComment) (ward *issueComment, human *issueComment) {
	for i := range comments {
		c := &comments[i]
		if wardAuthoredComment(*c) {
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
	ward, human := latestWardAndHumanComments(comments)
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
	comments, err := cl.ListIssueComments(ctx, owner, repo, index)
	if err != nil {
		return fmt.Errorf("%s: read comment thread: %w", label, err)
	}
	if reason, blocked := humanInterventionBlockReason(comments, snapshot); blocked {
		reportHumanInterventionBlock(ctx, owner, repo, index, index, reason, cl, cl)
		return fmt.Errorf("%s: %s", label, reason)
	}
	return nil
}
