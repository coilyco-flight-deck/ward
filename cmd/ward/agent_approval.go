package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// Planning is read-only. Only the authenticated director broker can turn a
// trusted collaborator's intent into an automation-authored exact snapshot.

func agentApprovalPlanCommand() *cli.Command {
	return &cli.Command{
		Name:      "approval-plan",
		Usage:     "Render the exact immutable actor-approval snapshot and trusted-collaborator intent body; writes nothing.",
		ArgsUsage: "<owner/repo#N>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "pr", Usage: "treat the target as a pull request instead of an issue"},
			&cli.StringSliceFlag{Name: "comment-id", Usage: "include this exact external comment ID in the snapshot; repeat for multiple comments"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.approval-plan",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentApprovalPlan(ctx, cmd)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func agentIssueApproveCommand() *cli.Command {
	return &cli.Command{
		Name:      "approve",
		Usage:     "Ask the authenticated director broker to verify one trusted intent and append an immutable approval snapshot.",
		ArgsUsage: "<owner/repo#N>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "pr", Usage: "treat the target as a pull request instead of an issue"},
			&cli.IntFlag{Name: "intent-comment-id", Required: true, Usage: "trusted collaborator's WARD-APPROVAL-INTENT comment ID"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.issue.approve",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentIssueApprove(ctx, cmd)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

func (r *Runner) runAgentApprovalPlan(ctx context.Context, c *cli.Command) error {
	const label = "ward agent approval-plan"
	ref, err := r.resolveForgejoApprovalRef(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	selectedIDs, err := approvalCommentIDsFromFlags(c.StringSlice("comment-id"))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	cl := r.hostForgejoClient(ctx)
	target, comments, err := loadForgejoApprovalTarget(ctx, cl, ref, c.Bool("pr"))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	plan, err := newActorApprovalPlan(target, comments, selectedIDs, loadActorAuthorityPolicy())
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	body, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: encode plan: %w", label, err)
	}
	_, err = fmt.Fprintf(agentCommandWriter(c), "%s\n", body)
	return err
}

func (r *Runner) runAgentIssueApprove(ctx context.Context, c *cli.Command) error {
	const label = "ward agent issue approve"
	ref, err := r.resolveForgejoApprovalRef(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	intentID := c.Int("intent-comment-id")
	if intentID <= 0 {
		return fmt.Errorf("%s: --intent-comment-id must be positive", label)
	}
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" {
		return fmt.Errorf("%s: %s is unset; approval mutation is available only through an attached director broker", label, envDispatchBrokerAddr)
	}
	if err := probeHostDispatchBroker(ctx, addr); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	kind := approvalTargetIssue
	if c.Bool("pr") {
		kind = approvalTargetPullRequest
	}
	req := dispatchBrokerRequest{
		Action:          dispatchActionApproval,
		Role:            roleDirector,
		Target:          ref.String(),
		TargetKind:      kind,
		IntentCommentID: intentID,
		Requester:       strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:           strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	body, err := sendDispatchBrokerListRequest(ctx, addr, req)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	defer func() { _ = body.Close() }()
	if _, err := io.Copy(agentCommandWriter(c), body); err != nil {
		return fmt.Errorf("%s: relay broker result: %w", label, err)
	}
	return nil
}

func (r *Runner) resolveForgejoApprovalRef(ctx context.Context, label, raw string) (agentIssueRef, error) {
	if strings.TrimSpace(raw) == "" {
		return agentIssueRef{}, fmt.Errorf("%s: a target ref is required", label)
	}
	ref, err := r.resolveAgentIssueRef(ctx, raw)
	if err != nil {
		return agentIssueRef{}, fmt.Errorf("%s: %w", label, err)
	}
	if ref.trackerOrDefault() != trackerForgejo {
		return agentIssueRef{}, fmt.Errorf("%s: %s is not a Forgejo ref; actor approval currently uses Ward's authenticated Forgejo broker", label, ref)
	}
	if err := prWorkflowOwnerScope(label, ref.Owner); err != nil {
		return agentIssueRef{}, err
	}
	return ref, nil
}

func approvalCommentIDsFromFlags(raw []string) ([]int, error) {
	ids := make([]int, 0, len(raw))
	for _, item := range raw {
		id, err := strconv.Atoi(strings.TrimSpace(item))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("--comment-id %q must be a positive integer", item)
		}
		ids = append(ids, id)
	}
	return canonicalApprovalCommentIDs(ids)
}

func loadForgejoApprovalTarget(ctx context.Context, cl *forgejoClient, ref agentIssueRef, pullRequest bool) (approvalTargetSnapshot, []issueComment, error) {
	if cl == nil {
		return approvalTargetSnapshot{}, nil, fmt.Errorf("approval target: Forgejo client is unavailable")
	}
	kind := approvalTargetIssue
	target := approvalTargetSnapshot{Ref: ref.String()}
	if pullRequest {
		kind = approvalTargetPullRequest
		pr, err := cl.GetPullRequest(ctx, ref.Owner, ref.Repo, ref.Number)
		if err != nil {
			return approvalTargetSnapshot{}, nil, fmt.Errorf("read pull request %s: %w", ref, err)
		}
		target.State = pr.State
		target.Author = pr.User.Login
		target.Title = pr.Title
		target.Body = pr.Body
		target.HeadSHA = pr.Head.SHA
		target.HeadRef = pr.Head.Ref
		target.BaseRef = pr.Base.Ref
	} else {
		issue, err := cl.GetIssue(ctx, ref.Owner, ref.Repo, ref.Number)
		if err != nil {
			return approvalTargetSnapshot{}, nil, fmt.Errorf("read issue %s: %w", ref, err)
		}
		target.State = issue.State
		target.Author = issue.User.Login
		target.Title = issue.Title
		target.Body = issue.Body
	}
	target.Kind = kind
	comments, err := cl.ListIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return approvalTargetSnapshot{}, nil, fmt.Errorf("read comments on %s: %w", ref, err)
	}
	return target, comments, nil
}

func approvalTargetFromIssue(ref agentIssueRef, issue *Issue) (approvalTargetSnapshot, error) {
	if issue == nil {
		return approvalTargetSnapshot{}, fmt.Errorf("actor admission: issue %s is missing", ref)
	}
	return approvalTargetSnapshot{
		Kind:   approvalTargetIssue,
		Ref:    ref.String(),
		State:  issue.State,
		Author: issue.User.Login,
		Title:  issue.Title,
		Body:   issue.Body,
	}, nil
}

func approvalTargetFromPRContext(ref agentIssueRef, pr agentPullRequestContext) approvalTargetSnapshot {
	return approvalTargetSnapshot{
		Kind:    approvalTargetPullRequest,
		Ref:     ref.String(),
		State:   pr.State,
		Author:  pr.Author,
		Title:   pr.Title,
		Body:    pr.Body,
		HeadSHA: pr.HeadSHA,
		HeadRef: pr.HeadRef,
		BaseRef: pr.BaseRef,
	}
}

func approvalTargetFromForgejoPR(ref agentIssueRef, pr *forgejoPullRequest) (approvalTargetSnapshot, error) {
	if pr == nil {
		return approvalTargetSnapshot{}, fmt.Errorf("actor admission: pull request %s is missing", ref)
	}
	return approvalTargetSnapshot{
		Kind:    approvalTargetPullRequest,
		Ref:     ref.String(),
		State:   pr.State,
		Author:  pr.User.Login,
		Title:   pr.Title,
		Body:    pr.Body,
		HeadSHA: pr.Head.SHA,
		HeadRef: pr.Head.Ref,
		BaseRef: pr.Base.Ref,
	}, nil
}

func (r *Runner) runDispatchBrokerApproval(ctx context.Context, conn net.Conn, req dispatchBrokerRequest) {
	body, err := r.execDispatchBrokerApproval(ctx, req)
	if err != nil {
		writeDispatchBrokerResponse(conn, err)
		return
	}
	writeDispatchBrokerResponse(conn, nil)
	_, _ = io.WriteString(conn, body)
}

func (r *Runner) execDispatchBrokerApproval(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	if err := validateDispatchBrokerApproval(req); err != nil {
		return "", err
	}
	ref, _ := parseAgentIssueRef(req.Target)
	pullRequest := req.TargetKind == approvalTargetPullRequest
	cl := r.hostForgejoClient(ctx)
	policy := loadActorAuthorityPolicy()
	if err := verifyForgejoAutomationActor(ctx, cl, policy); err != nil {
		return "", err
	}
	target, comments, err := loadForgejoApprovalTarget(ctx, cl, ref, pullRequest)
	if err != nil {
		return "", err
	}
	record, err := actorApprovalFromIntent(target, comments, req.IntentCommentID, policy)
	if err != nil {
		return "", err
	}
	body, err := renderActorApprovalRecord(record.IntentCommentID, record.Approver, record.Snapshot)
	if err != nil {
		return "", err
	}
	if err := postExactForgejoIssueComment(ctx, cl, ref, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("approved: %s\nsnapshot-sha256: %s\nintent-comment-id: %d\n", ref, record.SnapshotSHA, record.IntentCommentID), nil
}

func verifyForgejoAutomationActor(ctx context.Context, cl *forgejoClient, policy actorAuthorityPolicy) error {
	if policy.Err != nil {
		return fmt.Errorf("automation actor verification: %w", policy.Err)
	}
	var current struct {
		Login string `json:"login"`
	}
	if _, err := cl.doJSON(ctx, http.MethodGet, []string{"user"}, nil, nil, false, &current); err != nil {
		return fmt.Errorf("automation actor verification: read authenticated Forgejo user: %w", err)
	}
	login := normalizeActorLogin(current.Login)
	if login == "" || login != policy.AutomationActor {
		return fmt.Errorf("automation actor verification: authenticated Forgejo user %q does not match configured automation actor %q", current.Login, policy.AutomationActor)
	}
	return nil
}

func validateDispatchBrokerApproval(req dispatchBrokerRequest) error {
	if strings.TrimSpace(req.Role) != roleDirector {
		return fmt.Errorf("dispatch broker: approval requires the director role")
	}
	if len(req.Argv) != 0 || req.Forgejo != nil {
		return fmt.Errorf("dispatch broker: approval accepts no argv or raw Forgejo request")
	}
	ref, err := parseAgentIssueRef(req.Target)
	if err != nil || ref.trackerOrDefault() != trackerForgejo {
		return fmt.Errorf("dispatch broker: approval target %q must be a Forgejo issue ref", req.Target)
	}
	if err := prWorkflowOwnerScope("dispatch broker approval", ref.Owner); err != nil {
		return err
	}
	if req.TargetKind != approvalTargetIssue && req.TargetKind != approvalTargetPullRequest {
		return fmt.Errorf("dispatch broker: approval target kind %q is unsupported", req.TargetKind)
	}
	if req.IntentCommentID <= 0 {
		return fmt.Errorf("dispatch broker: approval requires a positive intent comment ID")
	}
	return nil
}

func postExactForgejoIssueComment(ctx context.Context, cl *forgejoClient, ref agentIssueRef, body string) error {
	if cl == nil {
		return fmt.Errorf("approval snapshot: Forgejo client is unavailable")
	}
	if len(body) > nativeForgejoRequestBodyLimit {
		return fmt.Errorf("approval snapshot: rendered record exceeds the Forgejo broker request limit")
	}
	if _, err := cl.doJSON(ctx, http.MethodPost,
		[]string{"repos", ref.Owner, ref.Repo, "issues", strconv.Itoa(ref.Number), "comments"},
		nil, map[string]string{"body": body}, true, nil); err != nil {
		return fmt.Errorf("approval snapshot: post on %s: %w", ref, err)
	}
	return nil
}
