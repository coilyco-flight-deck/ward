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

type prActorAdmissionState struct {
	PR             *forgejoPullRequest
	Comments       []issueComment
	LinkedIssue    *Issue
	LinkedComments []issueComment
}

func wardAuthoredComment(c issueComment) bool {
	return classifyActorComment(c).Class == actorClassTrustedMachine
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
	state, err := loadPRActorAdmissionState(ctx, cl, owner, repo, index)
	if err != nil {
		return fmt.Errorf("%s: actor admission: %w", label, err)
	}
	comments := state.Comments
	snapshot = laterTime(snapshot, state.PR.UpdatedAt)
	issueNum := index
	if state.LinkedIssue != nil {
		comments = append(append([]issueComment(nil), state.LinkedComments...), comments...)
		snapshot = laterTime(snapshot, state.LinkedIssue.UpdatedAt)
		issueNum = state.LinkedIssue.Number
	}
	if reason, blocked := humanInterventionBlockReason(comments, snapshot); blocked {
		reportHumanInterventionBlock(ctx, owner, repo, issueNum, index, reason, cl, cl)
		return fmt.Errorf("%s: %s", label, reason)
	}
	return nil
}

func prWorkflowActorAdmissionGuard(ctx context.Context, cl *forgejoClient, owner, repo string, index int, label string) error {
	if _, err := loadPRActorAdmissionState(ctx, cl, owner, repo, index); err != nil {
		return fmt.Errorf("%s: actor admission: %w", label, err)
	}
	return nil
}

func loadPRActorAdmissionState(ctx context.Context, cl *forgejoClient, owner, repo string, index int) (prActorAdmissionState, error) {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return prActorAdmissionState{}, fmt.Errorf("read pull request: %w", err)
	}
	comments, err := cl.ListIssueComments(ctx, owner, repo, index)
	if err != nil {
		return prActorAdmissionState{}, fmt.Errorf("read complete pull request comment thread: %w", err)
	}
	ref := agentIssueRef{Owner: owner, Repo: repo, Number: index, MergeRequest: true}
	target, err := approvalTargetFromForgejoPR(ref, pr)
	if err != nil {
		return prActorAdmissionState{}, err
	}
	admitted, err := admitActorContent(target, comments, loadActorAuthorityPolicy())
	if err != nil {
		return prActorAdmissionState{}, fmt.Errorf("pull request %s: %w", ref, err)
	}
	state := prActorAdmissionState{PR: pr, Comments: comments}
	linked, ok := directorLinkedIssueNumber(owner, repo, admitted.Target.Body)
	if !ok {
		return state, nil
	}
	issue, err := cl.GetIssue(ctx, owner, repo, linked)
	if err != nil {
		return prActorAdmissionState{}, fmt.Errorf("read linked issue %s/%s#%d: %w", owner, repo, linked, err)
	}
	linkedComments, err := cl.ListIssueComments(ctx, owner, repo, linked)
	if err != nil {
		return prActorAdmissionState{}, fmt.Errorf("read complete linked issue comment thread: %w", err)
	}
	linkedRef := agentIssueRef{Owner: owner, Repo: repo, Number: linked}
	linkedTarget, err := approvalTargetFromIssue(linkedRef, issue)
	if err != nil {
		return prActorAdmissionState{}, err
	}
	if _, err := admitActorContent(linkedTarget, linkedComments, loadActorAuthorityPolicy()); err != nil {
		return prActorAdmissionState{}, fmt.Errorf("linked issue %s: %w", linkedRef, err)
	}
	state.LinkedIssue = issue
	state.LinkedComments = linkedComments
	return state, nil
}

func humanFeedbackOutcomeBlocked(comments []issueComment, snapshot time.Time) bool {
	_, blocked := humanInterventionBlockReason(comments, snapshot)
	return blocked
}
