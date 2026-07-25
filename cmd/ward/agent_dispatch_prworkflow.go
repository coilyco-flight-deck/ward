package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
)

// agent_dispatch_prworkflow.go is the host half of the ward#1067 PR-workflow
// broker actions; the permission gate re-runs host-side. docs/agent-pr-workflow.md.

// runDispatchBrokerPRWorkflow validates, gates, and executes one PR-workflow
// action, then streams the rendered body back like the list/logs actions do.
func (r *Runner) runDispatchBrokerPRWorkflow(ctx context.Context, conn net.Conn, req dispatchBrokerRequest) {
	body, err := r.execDispatchBrokerPRWorkflow(ctx, req)
	if err != nil {
		writeDispatchBrokerResponse(conn, "", "", "", err)
		return
	}
	writeDispatchBrokerResponse(conn, "", "", "", nil)
	_, _ = io.WriteString(conn, body)
}

// execDispatchBrokerPRWorkflow validates and executes one action on the host's
// core Forgejo client.
func (r *Runner) execDispatchBrokerPRWorkflow(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	return execDispatchBrokerPRWorkflowWith(ctx, r.hostForgejoClient(ctx), req)
}

// execDispatchBrokerPRWorkflowWith is the injectable core: shape validation, the
// embedded permission gate, then the shared executor the direct CLI path uses.
func execDispatchBrokerPRWorkflowWith(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	if err := validateDispatchBrokerPRWorkflow(req); err != nil {
		return "", err
	}
	if handler, ok := prWorkflowDispatchExecutors[dispatchAction(req.Action)]; ok {
		return handler(ctx, cl, req)
	}
	return "", fmt.Errorf("dispatch broker: action %q is not a PR-workflow action", req.Action)
}

type prWorkflowDispatchExecutor func(context.Context, *forgejoClient, dispatchBrokerRequest) (string, error)

var prWorkflowDispatchExecutors = map[string]prWorkflowDispatchExecutor{
	dispatchActionPRStatus:  execDispatchBrokerPRStatus,
	dispatchActionPRLogs:    execDispatchBrokerPRLogs,
	dispatchActionPRMerge:   execDispatchBrokerPRMerge,
	dispatchActionPRClose:   execDispatchBrokerPRClose,
	dispatchActionPRReopen:  execDispatchBrokerPRReopen,
	dispatchActionPRRecover: execDispatchBrokerPRRecover,
	dispatchActionCIRuns:    execDispatchBrokerCIRuns,
	dispatchActionCIRerun:   execDispatchBrokerCIRerun,
}

func execDispatchBrokerPRStatus(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	ref, err := parseAgentIssueRef(req.Target)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: %s target: %w", dispatchActionPRStatus, err)
	}
	if err := prWorkflowPermitted(strings.TrimSpace(req.Role), "", prOpStatus); err != nil {
		return "", fmt.Errorf("dispatch broker: %w", err)
	}
	body, err := prWorkflowStatusBodyWithHead(ctx, cl, ref.Owner, ref.Repo, ref.Number, strings.TrimSpace(req.Head), strings.EqualFold(strings.TrimSpace(req.Format), "json"))
	if err != nil {
		return "", err
	}
	return body, nil
}

func execDispatchBrokerPRLogs(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	ref, err := parseAgentIssueRef(req.Target)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: %s target: %w", dispatchActionPRLogs, err)
	}
	if err := prWorkflowPermitted(strings.TrimSpace(req.Role), "", prOpLogs); err != nil {
		return "", fmt.Errorf("dispatch broker: %w", err)
	}
	return prWorkflowLogsDirect(ctx, cl, ref.Owner, ref.Repo, ref.Number, strings.TrimSpace(req.Context))
}

func execDispatchBrokerPRMerge(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	ref, err := parseAgentIssueRef(req.Target)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: %s target: %w", dispatchActionPRMerge, err)
	}
	// The merge gate needs the PR's workflow marker, so it runs inside the
	// executor - after the PR body is in hand, before any mutation.
	return prWorkflowMergeExec(ctx, cl, strings.TrimSpace(req.Role), ref.Owner, ref.Repo, ref.Number, req.MergeStyle)
}

func execDispatchBrokerPRClose(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	ref, err := parseAgentIssueRef(req.Target)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: %s target: %w", dispatchActionPRClose, err)
	}
	return prWorkflowCloseExec(ctx, cl, strings.TrimSpace(req.Role), ref.Owner, ref.Repo, ref.Number, req.Reason, req.Supersedes)
}

func execDispatchBrokerPRReopen(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	ref, err := parseAgentIssueRef(req.Target)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: %s target: %w", dispatchActionPRReopen, err)
	}
	return prWorkflowReopenExec(ctx, cl, strings.TrimSpace(req.Role), ref.Owner, ref.Repo, ref.Number)
}

func execDispatchBrokerPRRecover(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	ref, err := parseAgentIssueRef(req.Target)
	if err != nil {
		return "", fmt.Errorf("dispatch broker: %s target: %w", dispatchActionPRRecover, err)
	}
	return prWorkflowRecoverReport(ctx, cl, strings.TrimSpace(req.Role), ref.Owner, ref.Repo, ref.Number)
}

func execDispatchBrokerCIRuns(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	owner, name, _ := strings.Cut(req.Target, "/")
	if err := prWorkflowPermitted(strings.TrimSpace(req.Role), "", prOpRuns); err != nil {
		return "", fmt.Errorf("dispatch broker: %w", err)
	}
	return prWorkflowRunsReport(ctx, cl, owner, name, req.Limit)
}

func execDispatchBrokerCIRerun(ctx context.Context, cl *forgejoClient, req dispatchBrokerRequest) (string, error) {
	owner, name, _ := strings.Cut(req.Target, "/")
	if err := prWorkflowPermitted(strings.TrimSpace(req.Role), "", prOpRerun); err != nil {
		return "", fmt.Errorf("dispatch broker: %w", err)
	}
	return prWorkflowRerunExec(ctx, cl, owner, name, req.RunID)
}

// validateDispatchBrokerPRWorkflow checks the ward#1067 request shape: no launch
// argv, a known embedded role, an in-scope coily* target, and per-action fields.
func validateDispatchBrokerPRWorkflow(req dispatchBrokerRequest) error {
	action := dispatchAction(req.Action)
	if !prWorkflowDispatchActions[action] {
		return fmt.Errorf("dispatch broker: action %q is not a PR-workflow action", req.Action)
	}
	if err := validateDispatchBrokerPRWorkflowShape(action, req); err != nil {
		return err
	}
	return validateDispatchBrokerPRWorkflowTarget(action, req)
}

func validateDispatchBrokerPRWorkflowShape(action string, req dispatchBrokerRequest) error {
	if len(req.Argv) != 0 {
		return fmt.Errorf("dispatch broker: %s takes no launch argv, got %v", action, req.Argv)
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		return fmt.Errorf("dispatch broker: %s requires the requesting role (fail-closed)", action)
	}
	if cat, err := cachedBuiltInAgentRoleCatalog(); err != nil {
		return fmt.Errorf("dispatch broker: %s: load embedded role catalog: %w", action, err)
	} else if _, ok := cat.Definitions[role]; !ok {
		return fmt.Errorf("dispatch broker: %s: role %q is not in ward's embedded role catalog - refusing fail-closed", action, role)
	}
	target := strings.TrimSpace(req.Target)
	if target == "" || strings.ContainsRune(target, '\x00') || strings.HasPrefix(target, "-") {
		return fmt.Errorf("dispatch broker: %s requires a well-formed target, got %q", action, target)
	}
	return nil
}

func validateDispatchBrokerPRWorkflowTarget(action string, req dispatchBrokerRequest) error {
	target := strings.TrimSpace(req.Target)
	switch action {
	case dispatchActionPRStatus, dispatchActionPRLogs, dispatchActionPRMerge:
		return validateDispatchBrokerPRRefTarget(action, target)
	case dispatchActionPRClose, dispatchActionPRReopen, dispatchActionPRRecover:
		return validateDispatchBrokerPRLifecycleTarget(action, target, req)
	case dispatchActionCIRuns, dispatchActionCIRerun:
		return validateDispatchBrokerCITarget(action, target, req)
	default:
		return nil
	}
}

func validateDispatchBrokerPRLifecycleTarget(action, target string, req dispatchBrokerRequest) error {
	ref, err := parseAgentIssueRef(target)
	if err != nil || ref.Owner == "" || ref.Repo == "" {
		return fmt.Errorf("dispatch broker: %s target %q is not an owner/repo#N pull-request ref", action, target)
	}
	if ref.trackerOrDefault() != trackerForgejo {
		return fmt.Errorf("dispatch broker: %s target %q is not a Forgejo ref", action, target)
	}
	if err := prWorkflowOwnerScope("dispatch broker: "+action, ref.Owner); err != nil {
		return err
	}
	if action == dispatchActionPRClose {
		if strings.TrimSpace(req.Reason) == "" && strings.TrimSpace(req.Supersedes) == "" {
			return fmt.Errorf("dispatch broker: %s requires a reason or superseding issue/PR reference", action)
		}
		if refText := strings.TrimSpace(req.Supersedes); refText != "" {
			if _, err := prWorkflowSupersedingRef(ref.Owner, ref.Repo, refText); err != nil {
				return fmt.Errorf("dispatch broker: %s supersedes ref %q is not a valid issue/PR reference: %w", action, refText, err)
			}
		}
	}
	return nil
}

// validateDispatchBrokerPRRefTarget checks the pr-status/pr-merge target: a
// Forgejo owner/repo#N ref on an in-scope owner.
func validateDispatchBrokerPRRefTarget(action, target string) error {
	ref, err := parseAgentIssueRef(target)
	if err != nil || ref.Owner == "" || ref.Repo == "" {
		return fmt.Errorf("dispatch broker: %s target %q is not an owner/repo#N pull-request ref", action, target)
	}
	if ref.trackerOrDefault() != trackerForgejo {
		return fmt.Errorf("dispatch broker: %s target %q is not a Forgejo ref", action, target)
	}
	return prWorkflowOwnerScope("dispatch broker: "+action, ref.Owner)
}

// validateDispatchBrokerCITarget checks the ci-runs/ci-rerun target: a bare
// owner/repo on an in-scope owner, plus the per-action numeric fields.
func validateDispatchBrokerCITarget(action, target string, req dispatchBrokerRequest) error {
	owner, name, ok := strings.Cut(target, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" || strings.ContainsAny(target, "# ") {
		return fmt.Errorf("dispatch broker: %s target %q is not an owner/repo", action, target)
	}
	if action == dispatchActionCIRuns && req.Limit < 0 {
		return fmt.Errorf("dispatch broker: %s limit must be >= 0", action)
	}
	if action == dispatchActionCIRerun && req.RunID <= 0 {
		return fmt.Errorf("dispatch broker: %s requires a positive run_id", action)
	}
	return prWorkflowOwnerScope("dispatch broker: "+action, owner)
}
