package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// transientWorkflowCommentCleanupKinds are the comment bodies that act as live
// control-plane state while a reservation or dispatch is in flight.
var transientWorkflowCommentCleanupKinds = map[string]struct{}{
	"reservation-held":  {},
	"dispatch-failed":   {},
	"dispatch-deferred": {},
}

// transientWorkflowComment reports whether a thread comment is live reservation or
// dispatch telemetry that should be removed once it is no longer active.
func transientWorkflowComment(body string) bool {
	for _, ln := range strings.Split(body, "\n") {
		if transientWorkflowCommentLine(ln) {
			return true
		}
	}
	return false
}

func transientWorkflowCommentLine(ln string) bool {
	raw := strings.TrimSpace(ln)
	if raw == "" {
		return false
	}
	upperRaw := strings.ToUpper(raw)
	if strings.Contains(upperRaw, strings.ToUpper(legacyWardReservationMarker)) {
		return true
	}
	if strings.Contains(upperRaw, strings.ToUpper(legacyWardDispatchMarker)) {
		return true
	}
	s := workflowCommentLine(raw)
	if s == "" {
		return false
	}
	if header, ok := parseWardedWorkflowCommentHeader(s); ok {
		_, transient := transientWorkflowCommentCleanupKinds[header.Variant]
		return transient
	}
	return false
}

// deleteTransientWorkflowComments removes stale transient reservation/dispatch
// comments from the issue thread up to cutoff. It is best-effort and idempotent.
func deleteTransientWorkflowComments(ctx context.Context, cl Tracker, ref agentIssueRef, cutoff time.Time) {
	if cl == nil {
		return
	}
	comments, err := cl.ListIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward transient comment cleanup: could not read comments on %s: %v\n", ref, err)
		return
	}
	for _, c := range comments {
		if c.ID == 0 || !transientWorkflowComment(c.Body) {
			continue
		}
		if !cutoff.IsZero() && c.CreatedAt.After(cutoff) {
			continue
		}
		if err := cl.DeleteIssueComment(ctx, ref.Owner, ref.Repo, c.ID); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "ward transient comment cleanup: could not delete comment %d on %s: %v\n", c.ID, ref, err)
		}
	}
}
