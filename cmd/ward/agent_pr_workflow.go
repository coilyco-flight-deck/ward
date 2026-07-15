package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_pr_workflow.go wires ward#1067: the native PR-workflow tools (merge,
// per-PR CI status, run status, rerun). See docs/agent-pr-workflow.md.

// prWorkflowOp names one native PR-workflow capability for the permission gate.
type prWorkflowOp string

const (
	prOpMerge   prWorkflowOp = "merge"
	prOpClose   prWorkflowOp = "close"
	prOpReopen  prWorkflowOp = "reopen"
	prOpRecover prWorkflowOp = "recover"
	prOpStatus  prWorkflowOp = "status"
	prOpRuns    prWorkflowOp = "runs"
	prOpRerun   prWorkflowOp = "rerun"
)

// prWorkflowRole resolves the acting role for a PR-workflow tool call: the
// container's WARD_ROLE when set, else the host operator's director lane.
func prWorkflowRole() string {
	if role := strings.TrimSpace(os.Getenv("WARD_ROLE")); role != "" {
		return role
	}
	return roleDirector
}

// prWorkflowPermitted is the embedded permission gate (ward#1067), answering
// from the shipped role catalog fail-closed. docs/agent-pr-workflow.md.
func prWorkflowPermitted(role string, wf workflowMode, op prWorkflowOp) error {
	cat, err := cachedBuiltInAgentRoleCatalog()
	if err != nil {
		return fmt.Errorf("pr %s: load embedded role catalog: %w", op, err)
	}
	def, ok := cat.Definitions[strings.TrimSpace(role)]
	if !ok {
		return fmt.Errorf("pr %s: role %q is not in ward's embedded role catalog (%s) - refusing fail-closed",
			op, role, strings.Join(builtInAgentRoleDefinitionOrder(), ", "))
	}
	return prWorkflowPermittedForDefinition(def, role, wf, op)
}

func prWorkflowPermittedForDefinition(def agentRoleDefinition, role string, wf workflowMode, op prWorkflowOp) error {
	switch op {
	case prOpStatus, prOpRuns, prOpRecover:
		return prWorkflowReadPermitted(def, role, op)
	case prOpRerun:
		return prWorkflowRerunPermitted(def, role, op)
	case prOpClose, prOpReopen, prOpMerge:
		return prMergePermitted(def, role, wf)
	default:
		return fmt.Errorf("pr: unknown PR-workflow op %q - refusing fail-closed", op)
	}
}

func prWorkflowReadPermitted(def agentRoleDefinition, role string, op prWorkflowOp) error {
	if !def.Capabilities.Has(semanticCapabilityRead) {
		return fmt.Errorf("pr %s: role %q lacks the read capability", op, role)
	}
	return nil
}

func prWorkflowRerunPermitted(def agentRoleDefinition, role string, op prWorkflowOp) error {
	if !def.Capabilities.Has(semanticCapabilityEngineering) && !def.Capabilities.Has(semanticCapabilityProjectManagement) {
		return fmt.Errorf("pr %s: role %q holds neither engineering nor project-management - rerun is withheld", op, role)
	}
	return nil
}

// prMergePermitted answers the merge half of the gate from the role's
// merge-authority grant in the embedded catalog.
func prMergePermitted(def agentRoleDefinition, role string, wf workflowMode) error {
	wf = wf.orDefault()
	for _, granted := range def.MergeAuthority {
		if granted == wf {
			return nil
		}
	}
	if len(def.MergeAuthority) == 0 {
		return fmt.Errorf("pr merge: role %q holds no merge authority in ward's embedded role catalog", role)
	}
	return fmt.Errorf("pr merge: role %q may not merge under workflow %s (its merge authority covers %s)",
		role, wf, workflowModesJoin(def.MergeAuthority))
}

func workflowModesJoin(modes []workflowMode) string {
	parts := make([]string, 0, len(modes))
	for _, m := range modes {
		parts = append(parts, string(m))
	}
	return strings.Join(parts, ", ")
}

// prWorkflowMarkerMode reads the workflow mode from a PR's body marker; no
// marker means the plain pull-request lane (only and-merge PRs are stamped).
func prWorkflowMarkerMode(body string) workflowMode {
	if marker, ok := directorPRWorkflowMarker(body); ok {
		return canonicalWorkflow(workflowMode(marker))
	}
	return workflowPullRequest
}

// --- shared executors (direct CLI path + host dispatch-broker handler) ---

// prWorkflowStatusReport renders the combined CI status for one PR head: the
// native GET /repos/{owner}/{repo}/commits/{ref}/status read (ward#1067).
func prWorkflowStatusReport(ctx context.Context, cl *forgejoClient, owner, repo string, index int) (string, error) {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", err
	}
	head := pr.HeadSHA()
	if head == "" {
		return "", fmt.Errorf("pr status: %s/%s#%d did not expose a head SHA", owner, repo, index)
	}
	combined, err := cl.GetCommitCombinedStatus(ctx, owner, repo, head)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	state := strings.ToLower(strings.TrimSpace(combined.State))
	if state == "" {
		state = "unknown"
	}
	fmt.Fprintf(&b, "%s/%s#%d %q head %s combined status: %s\n", owner, repo, index, pr.Title, head, state)
	for _, st := range combined.Statuses {
		fmt.Fprintf(&b, "  %s = %s\n", st.Context, strings.ToLower(st.EffectiveState()))
	}
	if len(combined.Statuses) == 0 {
		b.WriteString("  (no status contexts reported yet)\n")
	}
	if strings.ToLower(strings.TrimSpace(combined.State)) != "success" {
		prCtx := agentPullRequestContext{
			State:        strings.TrimSpace(pr.State),
			Title:        strings.TrimSpace(pr.Title),
			Body:         strings.TrimSpace(pr.Body),
			URL:          strings.TrimSpace(pr.HTMLURL),
			HeadSHA:      head,
			HeadRef:      strings.TrimSpace(pr.Head.Ref),
			BaseRef:      strings.TrimSpace(pr.Base.Ref),
			Mergeability: fmt.Sprintf("mergeable=%t", pr.Mergeable),
		}
		if assessment, aerr := classifyForgejoPRRepair(ctx, cl, owner, repo, prCtx); aerr == nil && assessment.Bucket != "" {
			fmt.Fprintf(&b, "  repair bucket: %s\n", assessment.Bucket)
			fmt.Fprintf(&b, "  repair note: %s\n", assessment.Note)
		}
	}
	// The required-context set is advisory here: the merge tool re-checks it
	// fail-closed. A branch read failure degrades to a note, not an error.
	if branch, berr := cl.GetBranch(ctx, owner, repo, pr.Base.Ref); berr == nil {
		if required := normalizeDirectorRequiredContexts(branch.StatusCheckContexts); len(required) > 0 {
			fmt.Fprintf(&b, "  required on %s: %s\n", pr.Base.Ref, strings.Join(required, ", "))
		}
	}
	return b.String(), nil
}

// prWorkflowMergeExec merges one PR through ward's compiled client: permission
// gate, live required-status gate, head-pinned merge, merged-state check.
func prWorkflowMergeExec(ctx context.Context, cl *forgejoClient, role, owner, repo string, index int, mergeStyle string) (string, error) {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", err
	}
	wf := prWorkflowMarkerMode(pr.Body)
	if err := prWorkflowPermitted(role, wf, prOpMerge); err != nil {
		return "", err
	}
	if note, ok, merr := prWorkflowAlreadyMergedOrSameTree(ctx, cl, owner, repo, index, pr.Base.Ref, pr.HeadSHA()); merr == nil && ok {
		return note, nil
	}
	if strings.ToLower(strings.TrimSpace(pr.State)) != "open" {
		return "", fmt.Errorf("pr merge: %s/%s#%d is %s, not open", owner, repo, index, pr.State)
	}
	head := pr.HeadSHA()
	if head == "" {
		return "", fmt.Errorf("pr merge: %s/%s#%d did not expose a head SHA", owner, repo, index)
	}
	if err := prWorkflowHumanInterventionGuard(ctx, cl, owner, repo, index, time.Time{}, "pr merge"); err != nil {
		return "", err
	}
	status, reason, ok := directorMergeStatusGate(ctx, cl, owner, repo, pr.Base.Ref, head)
	if !ok {
		return "", fmt.Errorf("pr merge: %s/%s#%d: %s", owner, repo, index, reason)
	}
	if prHasEmptyDiff(pr) {
		return fmt.Sprintf("pr merge: %s/%s#%d already matches %s (empty diff); no Forgejo merge endpoint call needed\n",
			owner, repo, index, pr.Base.Ref), nil
	}
	settledHead, err := mergePullRequestWithHeadAndStyleSettled(ctx, cl, owner, repo, index, head, mergeStyle, &status)
	if err != nil {
		return "", fmt.Errorf("pr merge: %s/%s#%d: %w", owner, repo, index, err)
	}
	head = settledHead
	if err := requirePullRequestMerged(ctx, cl, owner, repo, index, head); err != nil {
		return "", fmt.Errorf("pr merge: %s/%s#%d: %w", owner, repo, index, err)
	}
	return fmt.Sprintf("merged %s/%s#%d (role %s, workflow %s, head %s, status %s); %s\n",
		owner, repo, index, role, wf.orDefault(), head, status.Status.summary(), "merged-state check: merged"), nil
}

func prWorkflowAlreadyMergedOrSameTree(ctx context.Context, cl *forgejoClient, owner, repo string, index int, baseRef, headSHA string) (string, bool, error) {
	merged, err := cl.PullRequestMerged(ctx, owner, repo, index)
	if err != nil {
		return "", false, err
	}
	if merged {
		return fmt.Sprintf("%s/%s#%d already merged, no action\n", owner, repo, index), true, nil
	}
	headSHA = strings.TrimSpace(headSHA)
	if headSHA == "" {
		return "", false, nil
	}
	if note, ok := prWorkflowSameTreeNoAction(ctx, cl, owner, repo, index, baseRef, headSHA); ok {
		return note, true, nil
	}
	return "", false, nil
}

func prWorkflowSameTreeNoAction(ctx context.Context, cl *forgejoClient, owner, repo string, index int, baseRef, headSHA string) (string, bool) {
	baseSHA, ok := prWorkflowBaseCommitSHA(ctx, cl, owner, repo, baseRef)
	if !ok || baseSHA == "" || strings.EqualFold(baseSHA, headSHA) {
		return "", false
	}
	headTree, ok := prWorkflowCommitTreeSHA(ctx, cl, owner, repo, headSHA)
	if !ok || headTree == "" {
		return "", false
	}
	baseTree, ok := prWorkflowCommitTreeSHA(ctx, cl, owner, repo, baseSHA)
	if !ok {
		return "", false
	}
	if headTree == baseTree {
		return fmt.Sprintf("%s/%s#%d already merged, no action\n", owner, repo, index), true
	}
	return "", false
}

func prWorkflowBaseCommitSHA(ctx context.Context, cl *forgejoClient, owner, repo, baseRef string) (string, bool) {
	branch, err := cl.GetBranch(ctx, owner, repo, strings.TrimSpace(baseRef))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(branch.Commit.ID), true
}

func prWorkflowCommitTreeSHA(ctx context.Context, cl *forgejoClient, owner, repo, sha string) (string, bool) {
	tree, err := cl.GetCommitTreeSHA(ctx, owner, repo, sha)
	if err != nil {
		return "", false
	}
	return tree, true
}

func prHasEmptyDiff(pr *forgejoPullRequest) bool {
	if pr == nil {
		return false
	}
	return pr.Additions == 0 && pr.Deletions == 0
}

func prWorkflowCloseExec(ctx context.Context, cl *forgejoClient, role, owner, repo string, index int, reason, supersedes string) (string, error) {
	wf, head, note, err := prWorkflowClosePrepare(ctx, cl, role, owner, repo, index, reason, supersedes)
	if err != nil {
		return "", err
	}
	if err := cl.ClosePullRequest(ctx, owner, repo, index); err != nil {
		return "", fmt.Errorf("pr close: %s/%s#%d: %w", owner, repo, index, err)
	}
	return prWorkflowCloseConfirm(ctx, cl, role, owner, repo, index, wf, head, note)
}

func prWorkflowClosePrepare(ctx context.Context, cl *forgejoClient, role, owner, repo string, index int, reason, supersedes string) (workflowMode, string, string, error) {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", "", "", err
	}
	wf := prWorkflowMarkerMode(pr.Body)
	if err := prWorkflowPermitted(role, wf, prOpClose); err != nil {
		return "", "", "", err
	}
	if strings.ToLower(strings.TrimSpace(pr.State)) != "open" {
		return "", "", "", fmt.Errorf("pr close: %s/%s#%d is %s, not open", owner, repo, index, pr.State)
	}
	merged, err := cl.PullRequestMerged(ctx, owner, repo, index)
	if err != nil {
		return "", "", "", fmt.Errorf("pr close: %s/%s#%d: merged-state check failed: %w", owner, repo, index, err)
	}
	if merged {
		return "", "", "", fmt.Errorf("pr close: %s/%s#%d is already merged", owner, repo, index)
	}
	if strings.TrimSpace(reason) == "" && strings.TrimSpace(supersedes) == "" {
		return "", "", "", fmt.Errorf("pr close: %s/%s#%d requires a reason or superseding issue/PR reference", owner, repo, index)
	}
	head := pr.HeadSHA()
	if head == "" {
		return "", "", "", fmt.Errorf("pr close: %s/%s#%d did not expose a head SHA", owner, repo, index)
	}
	if err := prWorkflowHumanInterventionGuard(ctx, cl, owner, repo, index, pr.UpdatedAt, "pr close"); err != nil {
		return "", "", "", err
	}
	superseding, err := prWorkflowSupersedingRef(owner, repo, supersedes)
	if err != nil {
		return "", "", "", fmt.Errorf("pr close: %s/%s#%d superseding ref %q is invalid: %w", owner, repo, index, supersedes, err)
	}
	return wf, head, prWorkflowCloseNote(strings.TrimSpace(reason), superseding), nil
}

func prWorkflowCloseConfirm(ctx context.Context, cl *forgejoClient, role, owner, repo string, index int, wf workflowMode, head, note string) (string, error) {
	after, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", fmt.Errorf("pr close: %s/%s#%d: postcondition read failed: %w", owner, repo, index, err)
	}
	if after.HeadSHA() != head {
		return "", fmt.Errorf("pr close: %s/%s#%d head changed from %s to %s", owner, repo, index, head, after.HeadSHA())
	}
	if strings.ToLower(strings.TrimSpace(after.State)) != "closed" {
		return "", fmt.Errorf("pr close: %s/%s#%d postcondition failed: state is %s, want closed", owner, repo, index, after.State)
	}
	afterMerged, err := cl.PullRequestMerged(ctx, owner, repo, index)
	if err != nil {
		return "", fmt.Errorf("pr close: %s/%s#%d: merged-state postcondition failed: %w", owner, repo, index, err)
	}
	if afterMerged {
		return "", fmt.Errorf("pr close: %s/%s#%d postcondition failed: merged-state still reports merged", owner, repo, index)
	}
	return fmt.Sprintf("closed %s/%s#%d (role %s, workflow %s, head before %s, head after %s, merged=%t, reason: %s)\n",
		owner, repo, index, role, wf.orDefault(), head, after.HeadSHA(), afterMerged, note), nil
}

func prWorkflowReopenExec(ctx context.Context, cl *forgejoClient, role, owner, repo string, index int) (string, error) {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", err
	}
	wf := prWorkflowMarkerMode(pr.Body)
	if err := prWorkflowPermitted(role, wf, prOpReopen); err != nil {
		return "", err
	}
	if strings.ToLower(strings.TrimSpace(pr.State)) != "closed" {
		return "", fmt.Errorf("pr reopen: %s/%s#%d is %s, not closed", owner, repo, index, pr.State)
	}
	merged, err := cl.PullRequestMerged(ctx, owner, repo, index)
	if err != nil {
		return "", fmt.Errorf("pr reopen: %s/%s#%d: merged-state check failed: %w", owner, repo, index, err)
	}
	if merged {
		return "", fmt.Errorf("pr reopen: %s/%s#%d is already merged", owner, repo, index)
	}
	head := pr.HeadSHA()
	if head == "" {
		return "", fmt.Errorf("pr reopen: %s/%s#%d did not expose a head SHA", owner, repo, index)
	}
	if err := prWorkflowHumanInterventionGuard(ctx, cl, owner, repo, index, pr.UpdatedAt, "pr reopen"); err != nil {
		return "", err
	}
	if err := cl.ReopenPullRequest(ctx, owner, repo, index); err != nil {
		return "", fmt.Errorf("pr reopen: %s/%s#%d: %w", owner, repo, index, err)
	}
	after, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", fmt.Errorf("pr reopen: %s/%s#%d: postcondition read failed: %w", owner, repo, index, err)
	}
	if after.HeadSHA() != head {
		return "", fmt.Errorf("pr reopen: %s/%s#%d head changed from %s to %s", owner, repo, index, head, after.HeadSHA())
	}
	if strings.ToLower(strings.TrimSpace(after.State)) != "open" {
		return "", fmt.Errorf("pr reopen: %s/%s#%d postcondition failed: state is %s, want open", owner, repo, index, after.State)
	}
	return fmt.Sprintf("reopened %s/%s#%d (role %s, workflow %s, head before %s, head after %s, merged=%t)\n",
		owner, repo, index, role, wf.orDefault(), head, after.HeadSHA(), merged), nil
}

func prWorkflowRecoverReport(ctx context.Context, cl *forgejoClient, role, owner, repo string, index int) (string, error) {
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", err
	}
	if err := prWorkflowPermitted(role, prWorkflowMarkerMode(pr.Body), prOpRecover); err != nil {
		return "", err
	}
	merged, err := cl.PullRequestMerged(ctx, owner, repo, index)
	if err != nil {
		return "", fmt.Errorf("pr recover: %s/%s#%d: merged-state check failed: %w", owner, repo, index, err)
	}
	linked, ok := directorLinkedIssueNumber(pr.Body)
	linkedText := "none"
	linkedState := ""
	if ok {
		linkedText = fmt.Sprintf("#%d", linked)
		if issue, ierr := cl.GetIssue(ctx, owner, repo, linked); ierr == nil {
			linkedState = strings.ToLower(strings.TrimSpace(issue.State))
		}
	}
	next := prWorkflowRecoverNextAction(strings.ToLower(strings.TrimSpace(pr.State)), merged, ok, linkedState, linked)
	return fmt.Sprintf("%s/%s#%d recovery report: state=%s, merged=%t, head=%s, linked issue=%s, next safe action: %s\n",
		owner, repo, index, strings.ToLower(strings.TrimSpace(pr.State)), merged, pr.HeadSHA(), linkedText, next), nil
}

func prWorkflowRecoverNextAction(state string, merged bool, linkedOK bool, linkedState string, linked int) string {
	if merged && linkedOK && linkedState == "open" {
		return fmt.Sprintf("the PR is merged, but the carried issue #%d is still open; repair the carried issue trailer or close the issue by hand", linked)
	}
	switch state {
	case "closed":
		if merged {
			return "no recovery action needed - the PR is already merged"
		}
		return "reopen the PR, then re-run status and merge"
	case "open":
		if merged {
			return "no recovery action needed - the PR is already merged"
		}
		return "merge the PR once the required checks are green"
	default:
		if merged {
			return "no recovery action needed - the PR is already merged"
		}
		return "inspect the PR state before acting"
	}
}

func prWorkflowCloseNote(reason, supersedes string) string {
	parts := make([]string, 0, 2)
	if reason != "" {
		parts = append(parts, reason)
	}
	if supersedes != "" {
		parts = append(parts, "superseded by "+supersedes)
	}
	return strings.Join(parts, "; ")
}

func prWorkflowSupersedingRef(owner, repo, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "#") {
		n, err := strconv.Atoi(strings.TrimPrefix(raw, "#"))
		if err != nil || n <= 0 {
			return "", fmt.Errorf("not a positive issue number")
		}
		return fmt.Sprintf("%s/%s#%d", owner, repo, n), nil
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return fmt.Sprintf("%s/%s#%d", owner, repo, n), nil
	}
	ref, err := parseAgentIssueRef(raw)
	if err != nil {
		return "", err
	}
	if ref.Owner == "" || ref.Repo == "" || ref.Number <= 0 {
		return "", fmt.Errorf("not an issue or pull request reference")
	}
	return ref.String(), nil
}

const prWorkflowMergeSettleAttempts = 3

var prWorkflowMergeSettleDelay = 150 * time.Millisecond

func mergePullRequestWithHeadAndStyleSettled(ctx context.Context, cl *forgejoClient, owner, repo string, index int, head, mergeStyle string, status *directorMergeStatusCheck) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= prWorkflowMergeSettleAttempts; attempt++ {
		err := cl.MergePullRequestWithHeadAndStyle(ctx, owner, repo, index, head, mergeStyle)
		if err == nil {
			return head, nil
		}
		lastErr = err
		if !forgejoMergeNeedsSettle(err) {
			return "", err
		}
		if attempt == prWorkflowMergeSettleAttempts {
			break
		}
		time.Sleep(time.Duration(attempt) * prWorkflowMergeSettleDelay)
		pr, err := cl.GetPullRequest(ctx, owner, repo, index)
		if err != nil {
			return "", fmt.Errorf("forgejo merge stayed on 405 after %d settle attempt(s), and the PR refresh failed: %w", attempt, err)
		}
		if strings.ToLower(strings.TrimSpace(pr.State)) != "open" {
			return "", fmt.Errorf("forgejo merge stayed on 405 after %d settle attempt(s), but %s/%s#%d is now %s", attempt, owner, repo, index, pr.State)
		}
		head = pr.HeadSHA()
		if head == "" {
			return "", fmt.Errorf("forgejo merge stayed on 405 after %d settle attempt(s), and %s/%s#%d no longer exposed a head SHA", attempt, owner, repo, index)
		}
		nextStatus, reason, ok := directorMergeStatusGate(ctx, cl, owner, repo, pr.Base.Ref, head)
		if !ok {
			return "", fmt.Errorf("forgejo merge stayed on 405 after %d settle attempt(s), and the PR is no longer eligible: %s", attempt, reason)
		}
		*status = nextStatus
	}
	if status != nil {
		return "", fmt.Errorf("forgejo merge stayed on 405 after %d settle attempt(s) while the PR remained green (%s); retry later or check the Forgejo merge queue: %w", prWorkflowMergeSettleAttempts, status.Status.summary(), lastErr)
	}
	return "", fmt.Errorf("forgejo merge stayed on 405 after %d settle attempt(s); retry later or check the Forgejo merge queue: %w", prWorkflowMergeSettleAttempts, lastErr)
}

func forgejoMergeNeedsSettle(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "forgejo merge pr returned 405") && strings.Contains(msg, "please try again later")
}

type prMergePostconditionError struct {
	Owner   string
	Repo    string
	Index   int
	State   string
	HeadSHA string
	Merged  bool
	Cause   error
}

func (e *prMergePostconditionError) Error() string {
	state := strings.ToLower(strings.TrimSpace(e.State))
	if state == "" {
		state = "unknown"
	}
	head := strings.TrimSpace(e.HeadSHA)
	if head == "" {
		head = "<unknown>"
	}
	base := fmt.Sprintf("merge postcondition failed for %s/%s#%d: state %s, merged=%t, head %s",
		e.Owner, e.Repo, e.Index, state, e.Merged, head)
	if e.Cause != nil {
		return base + ": " + firstLine(e.Cause.Error())
	}
	return base
}

func (e *prMergePostconditionError) Unwrap() error { return e.Cause }

func (e *prMergePostconditionError) closedButUnmerged() bool {
	return strings.EqualFold(strings.TrimSpace(e.State), "closed") && !e.Merged && e.Cause == nil
}

func requirePullRequestMerged(ctx context.Context, cl *forgejoClient, owner, repo string, index int, expectedHead string) error {
	merged, err := cl.PullRequestMerged(ctx, owner, repo, index)
	if err != nil {
		return &prMergePostconditionError{
			Owner:   owner,
			Repo:    repo,
			Index:   index,
			State:   "unknown",
			HeadSHA: expectedHead,
			Cause:   err,
		}
	}
	if merged {
		return nil
	}
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return &prMergePostconditionError{
			Owner:   owner,
			Repo:    repo,
			Index:   index,
			State:   "unknown",
			HeadSHA: expectedHead,
			Merged:  false,
			Cause:   err,
		}
	}
	head := strings.TrimSpace(pr.HeadSHA())
	if head == "" {
		head = strings.TrimSpace(expectedHead)
	}
	return &prMergePostconditionError{
		Owner:   owner,
		Repo:    repo,
		Index:   index,
		State:   strings.TrimSpace(pr.State),
		HeadSHA: head,
		Merged:  false,
	}
}

// prWorkflowRunsReport renders a repo's Actions runs with per-run conclusions -
// the native list the rerun decision reads (ward#1067).
func prWorkflowRunsReport(ctx context.Context, cl *forgejoClient, owner, repo string, limit int) (string, error) {
	runs, err := cl.ListActionRuns(ctx, owner, repo, limit)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return fmt.Sprintf("%s/%s has no Actions runs\n", owner, repo), nil
	}
	var b strings.Builder
	for _, run := range runs {
		fmt.Fprintf(&b, "run %d (#%d) %s  %s %s %s %q\n",
			run.ID, run.IndexInRepo, strings.ToLower(strings.TrimSpace(run.Status)),
			run.WorkflowID, run.Event, run.PrettyRef, run.Title)
	}
	return b.String(), nil
}

// prWorkflowRerunExec asks the forge to rerun one Actions run natively; the
// forge-gap fallback (agentic-os#434) surfaces as a loud, actionable error.
func prWorkflowRerunExec(ctx context.Context, cl *forgejoClient, owner, repo string, runID int64) (string, error) {
	run, err := cl.GetActionRun(ctx, owner, repo, runID)
	if err != nil {
		return "", err
	}
	if err := cl.RerunActionRun(ctx, owner, repo, runID); err != nil {
		return "", err
	}
	return fmt.Sprintf("rerun requested for %s/%s run %d (%s, was %s)\n",
		owner, repo, runID, run.WorkflowID, strings.ToLower(strings.TrimSpace(run.Status))), nil
}

// --- the `ward agent pr` command group ---

// agentPRCommand builds `ward agent pr`: the native PR-workflow verbs. Meta
// verbs like stop/list/logs, not a startup role.
func agentPRCommand() *cli.Command {
	return &cli.Command{
		Name:  "pr",
		Usage: "Native PR-workflow tools: close, reopen, recover, merge, per-PR CI status, Actions run status, rerun - compiled ward capabilities gated by the embedded role x workflow permission table (ward#1067).",
		Description: `pr carries the PR-workflow management verbs as native ward code on the compiled
Forgejo client, gated by ward's embedded role permission system - not by the
runtime KDL specgen surface, so they keep working with the '.ward/' guardfile
bundle absent or rolled back (infrastructure#538).

Merge authority is keyed to the workflow-mode model in the embedded role
catalog: the director merges under pull-request and pull-request-and-merge,
the engineer self-merges under pull-request-and-merge only, and remote-branch-only
withholds merge from everyone. Status and runs are read verbs; rerun needs an
engineering or project-management role.

Close and reopen use the same workflow-mode gate as merge, with the close verb
requiring a reason or superseding issue/PR reference. Recover is read-only
diagnostics for the closed-unmerged case, reporting the head SHA, linked issue,
and the next safe action.

On a read-only director surface each verb forwards through the host dispatch
broker (the same TCP + token channel as stop/list/logs) and the host re-checks
the permission gate; elsewhere it runs in-process against the forge API.

  ward agent pr status coilyco-flight-deck/ward#123   # combined CI status for the PR head
  ward agent pr close  coilyco-flight-deck/ward#123 --reason "superseded by #1163"
  ward agent pr reopen coilyco-flight-deck/ward#123   # reopen a closed-unmerged PR
  ward agent pr recover coilyco-flight-deck/ward#123  # diagnose a closed-unmerged PR
  ward agent pr merge  coilyco-flight-deck/ward#123   # gate, merge pinned to the checked head, confirm merged
  ward agent pr runs   coilyco-flight-deck/ward       # Actions runs + per-run conclusion
  ward agent pr rerun  coilyco-flight-deck/ward 6778  # rerun one Actions run by id

See docs/agent-pr-workflow.md.`,
		Commands: []*cli.Command{
			agentPRStatusCommand(),
			agentPRCloseCommand(),
			agentPRReopenCommand(),
			agentPRRecoverCommand(),
			agentPRMergeCommand(),
			agentPRRunsCommand(),
			agentPRRerunCommand(),
		},
	}
}

func agentPRStatusCommand() *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "Read one PR head's combined CI status natively (GET /repos/{owner}/{repo}/commits/{ref}/status).",
		ArgsUsage: "<owner/repo#N | #N>",
		Action:    prWorkflowAction("agent.pr.status", runAgentPRStatus),
	}
}

func agentPRMergeCommand() *cli.Command {
	return &cli.Command{
		Name:      "merge",
		Usage:     "Merge one PR natively: embedded role x workflow gate, live status gate, head-pinned merge, merge-style selection, merged-state check.",
		ArgsUsage: "<owner/repo#N | #N>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "style", Usage: "Forgejo merge style: merge, squash, fast-forward-only, rebase, or rebase-merge (default: smart-defaults pr-merge-style when set, else repo default_merge_style when allowed)"},
		},
		Action: prWorkflowAction("agent.pr.merge", runAgentPRMerge),
	}
}

func agentPRCloseCommand() *cli.Command {
	return &cli.Command{
		Name:      "close",
		Usage:     "Close one PR natively with a required reason or superseding issue/PR ref.",
		ArgsUsage: "<owner/repo#N | #N>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "reason", Usage: "why the PR is being closed"},
			&cli.StringFlag{Name: "supersedes", Usage: "superseding issue or PR reference"},
		},
		Action: prWorkflowAction("agent.pr.close", runAgentPRClose),
	}
}

func agentPRReopenCommand() *cli.Command {
	return &cli.Command{
		Name:      "reopen",
		Usage:     "Reopen one closed-unmerged PR natively.",
		ArgsUsage: "<owner/repo#N | #N>",
		Action:    prWorkflowAction("agent.pr.reopen", runAgentPRReopen),
	}
}

func agentPRRecoverCommand() *cli.Command {
	return &cli.Command{
		Name:      "recover",
		Usage:     "Diagnose one closed-unmerged PR natively.",
		ArgsUsage: "<owner/repo#N | #N>",
		Action:    prWorkflowAction("agent.pr.recover", runAgentPRRecover),
	}
}

func agentPRRunsCommand() *cli.Command {
	return &cli.Command{
		Name:      "runs",
		Usage:     "List a repo's Actions runs with per-run conclusions natively.",
		ArgsUsage: "[owner/repo]",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Value: 20, Usage: "runs to list, newest first"},
		},
		Action: prWorkflowAction("agent.pr.runs", runAgentPRRuns),
	}
}

func agentPRRerunCommand() *cli.Command {
	return &cli.Command{
		Name:      "rerun",
		Usage:     "Rerun one failed Actions run natively (the agentic-os#434 forge gap degrades loudly, never silently).",
		ArgsUsage: "<owner/repo> <run-id>",
		Action:    prWorkflowAction("agent.pr.rerun", runAgentPRRerun),
	}
}

// prWorkflowAction wraps one pr leaf in the shared audit shape the other agent
// meta verbs use.
func prWorkflowAction(name string, action func(context.Context, *Runner, *cli.Command) error) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		return r.WrapVerb(verb.Spec{
			Name:       name,
			SkipPolicy: true, // forge API verbs; no repo tree to gate
			Action:     func(ctx context.Context, cmd *cli.Command) error { return action(ctx, r, cmd) },
		}, r.Audit)(ctx, c)
	}
}

// prWorkflowOwnerScope mirrors the broker/ops owner gate: these compiled verbs
// stay scoped to the coily* fleet owners like every other operator surface.
func prWorkflowOwnerScope(label, owner string) error {
	if !strings.HasPrefix(owner, brokerOwnerPrefix) {
		return fmt.Errorf("%s: owner %q is out of scope; restricted to %s* owners", label, owner, brokerOwnerPrefix)
	}
	return nil
}

// resolveAgentPRRef resolves a PR ref argument to a Forgejo owner/repo#N.
func (r *Runner) resolveAgentPRRef(ctx context.Context, label, arg string) (agentIssueRef, error) {
	if strings.TrimSpace(arg) == "" {
		return agentIssueRef{}, fmt.Errorf("%s: a PR ref is required: owner/repo#N or a bare #N from inside a checkout", label)
	}
	ref, err := r.resolveAgentIssueRef(ctx, arg)
	if err != nil {
		return agentIssueRef{}, fmt.Errorf("%s: %w", label, err)
	}
	if ref.trackerOrDefault() != trackerForgejo {
		return agentIssueRef{}, fmt.Errorf("%s: %s is not a Forgejo ref - the native PR-workflow tools drive the Forgejo API only (GitHub PRs go through `gh`)", label, ref)
	}
	if err := prWorkflowOwnerScope(label, ref.Owner); err != nil {
		return agentIssueRef{}, err
	}
	return ref, nil
}

// resolveAgentPRRepo resolves an optional owner/repo argument, falling back to
// the container target repo, then the cwd git origin.
func (r *Runner) resolveAgentPRRepo(ctx context.Context, label, arg string) (owner, name string, err error) {
	slug := strings.TrimSpace(arg)
	if slug == "" {
		slug = strings.TrimSpace(os.Getenv("WARD_TARGET_REPO"))
	}
	if slug == "" {
		repo, _, terr := r.resolveTarget(ctx, "")
		if terr != nil {
			return "", "", fmt.Errorf("%s: no owner/repo given and the cwd has no git origin to infer one from: %w", label, terr)
		}
		slug = repo.slug()
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("%s: %q is not an owner/repo", label, slug)
	}
	if err := prWorkflowOwnerScope(label, owner); err != nil {
		return "", "", err
	}
	return owner, name, nil
}

// prWorkflowForwarded relays the verb through the host dispatch broker when the
// session is a read-only surface with one attached; handled=false runs direct.
func prWorkflowForwarded(ctx context.Context, r *Runner, req dispatchBrokerRequest) (bool, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return false, nil
	}
	if err := probeHostDispatchBroker(ctx, addr); err != nil {
		return true, err
	}
	req.Role = prWorkflowRole()
	req.Requester = strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME"))
	req.Token = strings.TrimSpace(os.Getenv(envDispatchBrokerToken))
	body, err := sendDispatchBrokerListRequest(ctx, addr, req)
	if err != nil {
		return true, err
	}
	defer func() { _ = body.Close() }()
	if _, err := io.Copy(r.Runner.Stdout, body); err != nil {
		return true, fmt.Errorf("ward agent pr: relay host output: %w", err)
	}
	return true, nil
}

func runAgentPRStatus(ctx context.Context, r *Runner, c *cli.Command) error {
	const label = "ward agent pr status"
	ref, err := r.resolveAgentPRRef(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	if handled, err := prWorkflowForwarded(ctx, r, dispatchBrokerRequest{
		Action: dispatchActionPRStatus, Target: ref.String(),
	}); handled {
		return err
	}
	if err := prWorkflowPermitted(prWorkflowRole(), "", prOpStatus); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	body, err := prWorkflowStatusReport(ctx, r.hostForgejoClient(ctx), ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	_, err = io.WriteString(r.Runner.Stdout, body)
	return err
}

func runAgentPRClose(ctx context.Context, r *Runner, c *cli.Command) error {
	const label = "ward agent pr close"
	ref, err := r.resolveAgentPRRef(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	if handled, err := prWorkflowForwarded(ctx, r, dispatchBrokerRequest{
		Action: dispatchActionPRClose, Target: ref.String(), Reason: c.String("reason"), Supersedes: c.String("supersedes"),
	}); handled {
		return err
	}
	body, err := prWorkflowCloseExec(ctx, r.hostForgejoClient(ctx), prWorkflowRole(), ref.Owner, ref.Repo, ref.Number, c.String("reason"), c.String("supersedes"))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	_, err = io.WriteString(r.Runner.Stdout, body)
	return err
}

func runAgentPRReopen(ctx context.Context, r *Runner, c *cli.Command) error {
	const label = "ward agent pr reopen"
	ref, err := r.resolveAgentPRRef(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	if handled, err := prWorkflowForwarded(ctx, r, dispatchBrokerRequest{
		Action: dispatchActionPRReopen, Target: ref.String(),
	}); handled {
		return err
	}
	body, err := prWorkflowReopenExec(ctx, r.hostForgejoClient(ctx), prWorkflowRole(), ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	_, err = io.WriteString(r.Runner.Stdout, body)
	return err
}

func runAgentPRRecover(ctx context.Context, r *Runner, c *cli.Command) error {
	const label = "ward agent pr recover"
	ref, err := r.resolveAgentPRRef(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	if handled, err := prWorkflowForwarded(ctx, r, dispatchBrokerRequest{
		Action: dispatchActionPRRecover, Target: ref.String(),
	}); handled {
		return err
	}
	body, err := prWorkflowRecoverReport(ctx, r.hostForgejoClient(ctx), prWorkflowRole(), ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	_, err = io.WriteString(r.Runner.Stdout, body)
	return err
}

func runAgentPRMerge(ctx context.Context, r *Runner, c *cli.Command) error {
	const label = "ward agent pr merge"
	ref, err := r.resolveAgentPRRef(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	if handled, err := prWorkflowForwarded(ctx, r, dispatchBrokerRequest{
		Action: dispatchActionPRMerge, Target: ref.String(), MergeStyle: c.String("style"),
	}); handled {
		return err
	}
	body, err := prWorkflowMergeExec(ctx, r.hostForgejoClient(ctx), prWorkflowRole(), ref.Owner, ref.Repo, ref.Number, c.String("style"))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	_, err = io.WriteString(r.Runner.Stdout, body)
	return err
}

func runAgentPRRuns(ctx context.Context, r *Runner, c *cli.Command) error {
	const label = "ward agent pr runs"
	owner, name, err := r.resolveAgentPRRepo(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	limit := c.Int("limit")
	if handled, err := prWorkflowForwarded(ctx, r, dispatchBrokerRequest{
		Action: dispatchActionCIRuns, Target: owner + "/" + name, Limit: limit,
	}); handled {
		return err
	}
	if err := prWorkflowPermitted(prWorkflowRole(), "", prOpRuns); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	body, err := prWorkflowRunsReport(ctx, r.hostForgejoClient(ctx), owner, name, limit)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	_, err = io.WriteString(r.Runner.Stdout, body)
	return err
}

func runAgentPRRerun(ctx context.Context, r *Runner, c *cli.Command) error {
	const label = "ward agent pr rerun"
	owner, name, err := r.resolveAgentPRRepo(ctx, label, c.Args().First())
	if err != nil {
		return err
	}
	runArg := strings.TrimSpace(c.Args().Get(1))
	if runArg == "" {
		return fmt.Errorf("%s: a run id is required (`ward agent pr runs %s/%s` lists them)", label, owner, name)
	}
	runID, err := strconv.ParseInt(runArg, 10, 64)
	if err != nil || runID <= 0 {
		return fmt.Errorf("%s: run id %q is not a positive number", label, runArg)
	}
	if handled, err := prWorkflowForwarded(ctx, r, dispatchBrokerRequest{
		Action: dispatchActionCIRerun, Target: owner + "/" + name, RunID: runID,
	}); handled {
		return err
	}
	if err := prWorkflowPermitted(prWorkflowRole(), "", prOpRerun); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	body, err := prWorkflowRerunExec(ctx, r.hostForgejoClient(ctx), owner, name, runID)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	_, err = io.WriteString(r.Runner.Stdout, body)
	return err
}
