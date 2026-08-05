package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	dispatchActionTrackerMutation = "tracker-mutation"

	trackerMutationComment       = "comment"
	trackerMutationDeleteComment = "delete-comment"
	trackerMutationCreateIssue   = "create-issue"
	trackerMutationCloseIssue    = "close-issue"
	trackerMutationReopenIssue   = "reopen-issue"
	trackerMutationCreatePR      = "create-pr"
)

type trackerMutationRequest struct {
	Operation  string `json:"operation"`
	RecordKind string `json:"record_kind"`
	Target     string `json:"target"`
	CommentID  int    `json:"comment_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	Head       string `json:"head,omitempty"`
	Base       string `json:"base,omitempty"`
}

type trackerMutationResult struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
}

func agentTrackerBrokerEnabled() bool {
	role := strings.TrimSpace(os.Getenv("WARD_ROLE"))
	return (role == roleEngineer || role == roleQA) &&
		strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)) != "" &&
		strings.TrimSpace(os.Getenv(envDispatchBrokerToken)) != ""
}

func sendAgentTrackerMutation(ctx context.Context, mutation trackerMutationRequest) (trackerMutationResult, error) {
	req := dispatchBrokerRequest{
		Action:    dispatchActionTrackerMutation,
		Role:      strings.TrimSpace(os.Getenv("WARD_ROLE")),
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
		Tracker:   &mutation,
	}
	result, err := sendDispatchBrokerForgejoRequest(ctx, strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)), req)
	if err != nil {
		return trackerMutationResult{}, err
	}
	var decoded trackerMutationResult
	if len(result.Body) > 0 {
		if err := json.Unmarshal(result.Body, &decoded); err != nil {
			return trackerMutationResult{}, fmt.Errorf("tracker mutation broker: decode result: %w", err)
		}
	}
	return decoded, nil
}

func (r *Runner) runDispatchBrokerTrackerMutation(ctx context.Context, conn net.Conn, req dispatchBrokerRequest) {
	result, err := r.execDispatchBrokerTrackerMutation(ctx, req)
	data, marshalErr := json.Marshal(result)
	if err == nil && marshalErr != nil {
		err = marshalErr
	}
	writeDispatchBrokerForgejoResponse(conn, nativeForgejoResult{Status: http.StatusOK, ContentType: "application/json", Body: data}, nativeForgejoErrorPolicy, err)
}

func (r *Runner) execDispatchBrokerTrackerMutation(ctx context.Context, req dispatchBrokerRequest) (trackerMutationResult, error) {
	if err := validateDispatchBrokerTrackerMutation(req); err != nil {
		return trackerMutationResult{}, err
	}
	mutation := *req.Tracker
	repo, ref, _ := trackerMutationTarget(mutation.Operation, mutation.Target)
	cl := r.hostForgejoClient(ctx)
	if err := verifyForgejoAutomationActor(ctx, cl, loadActorAuthorityPolicy()); err != nil {
		return trackerMutationResult{}, err
	}
	return execTrackerMutationOperation(ctx, cl, repo, ref, mutation)
}

func execTrackerMutationOperation(ctx context.Context, cl *forgejoClient, repo targetRepo, ref agentIssueRef, mutation trackerMutationRequest) (trackerMutationResult, error) {
	switch mutation.Operation {
	case trackerMutationComment:
		if err := postExactForgejoIssueComment(ctx, cl, ref, mutation.Body); err != nil {
			return trackerMutationResult{}, err
		}
	case trackerMutationDeleteComment:
		if err := deleteTransientTrackerComment(ctx, cl, repo, mutation.CommentID); err != nil {
			return trackerMutationResult{}, err
		}
	case trackerMutationCreateIssue:
		return createTrackerIssue(ctx, cl, repo, mutation)
	case trackerMutationCloseIssue, trackerMutationReopenIssue:
		if err := setTrackerIssueState(ctx, cl, ref, mutation.Operation); err != nil {
			return trackerMutationResult{}, err
		}
	case trackerMutationCreatePR:
		return createTrackerPullRequest(ctx, cl, repo, mutation)
	}
	return trackerMutationResult{}, nil
}

func deleteTransientTrackerComment(ctx context.Context, cl *forgejoClient, repo targetRepo, commentID int) error {
	var comment issueComment
	if _, err := cl.doJSON(ctx, http.MethodGet,
		[]string{"repos", repo.Owner, repo.Name, "issues", "comments", strconv.Itoa(commentID)},
		nil, nil, false, &comment); err != nil {
		return fmt.Errorf("tracker mutation: read comment %d before deletion: %w", commentID, err)
	}
	admission := classifyActorCommentWithPolicy(comment, loadActorAuthorityPolicy())
	if admission.Class != actorClassTrustedMachine || !transientTrackerRecordKind(admission.RecordKind) {
		return fmt.Errorf("tracker mutation: comment %d is not deletable transient machine state", commentID)
	}
	_, err := cl.doJSON(ctx, http.MethodDelete,
		[]string{"repos", repo.Owner, repo.Name, "issues", "comments", strconv.Itoa(commentID)},
		nil, nil, true, nil)
	return err
}

func transientTrackerRecordKind(kind string) bool {
	return kind == recordKindReservation || kind == recordKindReservationRelease || kind == recordKindDispatch
}

func createTrackerIssue(ctx context.Context, cl *forgejoClient, repo targetRepo, mutation trackerMutationRequest) (trackerMutationResult, error) {
	var created struct {
		Number int `json:"number"`
	}
	if _, err := cl.doJSON(ctx, http.MethodPost, []string{"repos", repo.Owner, repo.Name, "issues"}, nil,
		map[string]string{"title": mutation.Title, "body": mutation.Body}, true, &created); err != nil {
		return trackerMutationResult{}, err
	}
	return trackerMutationResult{Number: created.Number}, nil
}

func setTrackerIssueState(ctx context.Context, cl *forgejoClient, ref agentIssueRef, operation string) error {
	state := "closed"
	if operation == trackerMutationReopenIssue {
		state = "open"
	}
	_, err := cl.doJSON(ctx, http.MethodPatch, []string{"repos", ref.Owner, ref.Repo, "issues", strconv.Itoa(ref.Number)}, nil,
		map[string]string{"state": state}, true, nil)
	return err
}

func createTrackerPullRequest(ctx context.Context, cl *forgejoClient, repo targetRepo, mutation trackerMutationRequest) (trackerMutationResult, error) {
	var created struct {
		HTMLURL string `json:"html_url"`
	}
	payload := map[string]any{"head": mutation.Head, "base": mutation.Base, "title": mutation.Title, "body": mutation.Body}
	if _, err := cl.doJSON(ctx, http.MethodPost, []string{"repos", repo.Owner, repo.Name, "pulls"}, nil, payload, true, &created); err != nil {
		return trackerMutationResult{}, err
	}
	return trackerMutationResult{URL: created.HTMLURL}, nil
}

func validateDispatchBrokerTrackerMutation(req dispatchBrokerRequest) error {
	if req.Tracker == nil {
		return fmt.Errorf("dispatch broker: tracker mutation payload is missing")
	}
	if len(req.Argv) != 0 || req.Forgejo != nil {
		return fmt.Errorf("dispatch broker: tracker mutation accepts no argv or raw Forgejo request")
	}
	mutation := req.Tracker
	repo, _, err := trackerMutationTarget(mutation.Operation, mutation.Target)
	if err != nil {
		return fmt.Errorf("dispatch broker: tracker mutation target %q: %w", mutation.Target, err)
	}
	if err := prWorkflowOwnerScope("dispatch broker tracker mutation", repo.Owner); err != nil {
		return err
	}
	role := strings.TrimSpace(req.AuthenticatedRole)
	if err := trackerMutationRoleAllowed(role, mutation.Operation, mutation.RecordKind); err != nil {
		return err
	}
	if len(mutation.Body) > nativeForgejoRequestBodyLimit {
		return fmt.Errorf("dispatch broker: tracker mutation body exceeds %d bytes", nativeForgejoRequestBodyLimit)
	}
	return validateTrackerMutationShape(*mutation)
}

func validateTrackerMutationShape(mutation trackerMutationRequest) error {
	switch mutation.Operation {
	case trackerMutationComment:
		return validateTrackerCommentShape(mutation)
	case trackerMutationDeleteComment:
		return validateTrackerDeleteShape(mutation)
	case trackerMutationCreateIssue:
		return validateTrackerCreateIssueShape(mutation)
	case trackerMutationCloseIssue, trackerMutationReopenIssue:
		return validateTrackerIssueStateShape(mutation)
	case trackerMutationCreatePR:
		return validateTrackerCreatePRShape(mutation)
	default:
		return fmt.Errorf("dispatch broker: tracker mutation operation %q is unsupported", mutation.Operation)
	}
}

func validateTrackerCommentShape(m trackerMutationRequest) error {
	if strings.TrimSpace(m.Body) == "" || m.Title != "" || m.Head != "" || m.Base != "" || m.CommentID != 0 {
		return fmt.Errorf("dispatch broker: comment mutation shape is invalid")
	}
	kind, recognized, _ := fixedWardRecordKind(m.Body)
	if !recognized || kind != m.RecordKind {
		return fmt.Errorf("dispatch broker: comment body record kind %q does not match authenticated kind %q", kind, m.RecordKind)
	}
	return nil
}

func validateTrackerDeleteShape(m trackerMutationRequest) error {
	if m.CommentID <= 0 || m.Title != "" || m.Body != "" || m.Head != "" || m.Base != "" {
		return fmt.Errorf("dispatch broker: delete-comment mutation shape is invalid")
	}
	return nil
}

func validateTrackerCreateIssueShape(m trackerMutationRequest) error {
	if strings.TrimSpace(m.Title) == "" || strings.TrimSpace(m.Body) == "" || m.CommentID != 0 || m.Head != "" || m.Base != "" {
		return fmt.Errorf("dispatch broker: create-issue mutation shape is invalid")
	}
	return nil
}

func validateTrackerIssueStateShape(m trackerMutationRequest) error {
	if m.CommentID != 0 || m.Title != "" || m.Body != "" || m.Head != "" || m.Base != "" {
		return fmt.Errorf("dispatch broker: issue-state mutation shape is invalid")
	}
	return nil
}

func validateTrackerCreatePRShape(m trackerMutationRequest) error {
	if strings.TrimSpace(m.Title) == "" || strings.TrimSpace(m.Body) == "" || strings.TrimSpace(m.Head) == "" || strings.TrimSpace(m.Base) == "" || m.CommentID != 0 {
		return fmt.Errorf("dispatch broker: create-pr mutation shape is invalid")
	}
	return nil
}

func trackerMutationTarget(operation, raw string) (targetRepo, agentIssueRef, error) {
	switch operation {
	case trackerMutationCreateIssue, trackerMutationCreatePR, trackerMutationDeleteComment:
		repo, err := parseRepoRef(raw)
		if err != nil {
			return targetRepo{}, agentIssueRef{}, fmt.Errorf("must be owner/repo: %w", err)
		}
		return repo, agentIssueRef{Owner: repo.Owner, Repo: repo.Name}, nil
	default:
		ref, err := parseAgentIssueRef(raw)
		if err != nil || ref.trackerOrDefault() != trackerForgejo {
			return targetRepo{}, agentIssueRef{}, fmt.Errorf("must be a Forgejo issue ref")
		}
		return targetRepo{Owner: ref.Owner, Name: ref.Repo}, ref, nil
	}
}

func trackerMutationRoleAllowed(role, operation, recordKind string) error {
	operations, ok := trackerMutationRolePolicy[role]
	if !ok || !operations[operation][recordKind] {
		return fmt.Errorf("dispatch broker: role %q cannot perform tracker mutation %q as record kind %q", role, operation, recordKind)
	}
	return nil
}

var trackerMutationRolePolicy = map[string]map[string]map[string]bool{
	roleQA: {
		trackerMutationComment: {recordKindQA: true},
	},
	roleEngineer: {
		trackerMutationComment: {
			recordKindReservation: true, recordKindReservationRelease: true, recordKindOutcome: true,
			recordKindPreflight: true, recordKindReview: true, recordKindRoute: true, recordKindDispatch: true,
		},
		trackerMutationDeleteComment: {recordKindReservation: true, recordKindReservationRelease: true, recordKindDispatch: true},
		trackerMutationCreateIssue:   {recordKindRoute: true, recordKindOutcome: true},
		trackerMutationCloseIssue:    {recordKindRoute: true, recordKindOutcome: true},
		trackerMutationReopenIssue:   {recordKindOutcome: true},
		trackerMutationCreatePR:      {recordKindOutcome: true},
	},
}
