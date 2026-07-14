package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// agent_director_merge.go wires `ward agent director merge`: the explicit PR-merge
// lane the director uses for ward-owned PRs that are authorized to land.

var directorClosingRefRE = regexp.MustCompile(`(?i)\b(?:closes|fixes|resolves)\s+#(\d+)\b`)
var directorWorkflowMarkerRE = regexp.MustCompile(`(?i)\bward\.workflow:\s*([[:alnum:]-]+)\b`)

const directorMergeConflictStaleAfter = 2 * time.Hour

// directorMergeFlags keeps the merge subcommand narrow: scope + preview only.
func directorMergeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: director.default-scope from ~/.ward/config.yaml)"},
		&cli.StringSliceFlag{Name: "org", Usage: "expand every repo an org owns into the scope (owner; repeatable), unioned with --repo and de-duped"},
		&cli.IntFlag{Name: "limit", Value: directorLimitDefault(), Usage: "open issues read per repo per refresh"},
		&cli.BoolFlag{Name: "dry-run", Usage: "show the PRs that would merge, then exit without merging"},
		&cli.BoolFlag{Name: "print", Usage: "alias for --dry-run"},
	}
}

// agentDirectorMergeCommand builds the explicit director merge lane.
func agentDirectorMergeCommand() *cli.Command {
	return &cli.Command{
		Name:        "merge",
		Usage:       "Merge eligible ward-owned PRs whose issue thread authorizes director merge.",
		ArgsUsage:   "(scope via --repo; default: director.default-scope from ~/.ward/config.yaml)",
		Description: `merge scans open pull requests in scope and merges only the ones the ward issue thread marks as director-merge authorized: the linked issue ended with WARDED_WORKFLOW: merge-ready or a URL-headed reviewed-and-ready handoff, the final comment says workflow: pull-request-and-merge, the review summary is passed, the PR is mergeable against the current base branch, and it is not salvage/draft noise. The director records the final done outcome only after the merge lands. pull-request still needs a human. See docs/agent-director.md and docs/agent-workflow.md.`,
		Flags:       directorMergeFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.runDirectorMerge(ctx, c)
		},
	}
}

// runDirectorMerge resolves scope, filters the open PR set down to merge-eligible
// candidates, and merges them one by one.
func (r *Runner) runDirectorMerge(ctx context.Context, c *cli.Command) error {
	label := "ward agent director merge"
	repos, err := r.resolveDirectorScope(ctx, c, label)
	if err != nil {
		return err
	}
	if err := r.backlogTrustGate(label, repos); err != nil {
		return err
	}
	prClient := r.hostForgejoClient(ctx)
	issueClient, err := r.hostTrackerClient(ctx, trackerForgejo, currentAgentMode())
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	preview := c.Bool("dry-run") || c.Bool("print")
	limit := c.Int("limit")
	if limit < 1 {
		limit = directorLimitDefault()
	}
	var merged, skipped int
	for _, repo := range repos {
		repoMerged, repoSkipped, err := r.runDirectorMergeRepo(ctx, label, prClient, issueClient, repo, limit, preview)
		if err != nil {
			return err
		}
		merged += repoMerged
		skipped += repoSkipped
	}
	_, _ = fmt.Fprintf(r.Runner.Stdout, "%s: merged %d PR(s), skipped %d\n", label, merged, skipped)
	return nil
}

func (r *Runner) runDirectorMergeRepo(ctx context.Context, label string, prClient *forgejoClient, issueClient Tracker, repo string, limit int, preview bool) (merged, skipped int, err error) {
	owner, name, _ := strings.Cut(repo, "/")
	prs, err := prClient.ListOpenPullRequests(ctx, owner, name, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", label, err)
	}
	for _, pr := range prs {
		ok, reason, linked, meta := directorMergeEligibility(ctx, owner, name, pr, prClient, issueClient)
		if !ok {
			skipped++
			_, _ = fmt.Fprintf(r.Runner.Stderr, "%s: skipping %s/%s#%d: %s\n", label, owner, name, pr.Number, reason)
			continue
		}
		if preview {
			_, _ = fmt.Fprintf(r.Runner.Stderr, "%s: would merge %s/%s#%d (issue #%d, workflow %s, review %q, head %s, status %s)\n",
				label, owner, name, pr.Number, linked, meta.Workflow, meta.Review, meta.PRHeadSHA, meta.Status.summary())
			continue
		}
		status := directorMergeStatusCheck{Status: meta.Status}
		mergedHead, err := mergeDirectorPullRequest(ctx, prClient, owner, name, pr.Number, meta.PRHeadSHA, "", &status)
		if err != nil {
			return merged, skipped, fmt.Errorf("%s: merge %s/%s#%d: %w", label, owner, name, pr.Number, err)
		}
		meta.Status = status.Status
		if err := recordDirectorMergeDone(ctx, issueClient, owner, name, linked, pr.Number, meta); err != nil {
			return merged, skipped, fmt.Errorf("%s: record done for %s/%s#%d after merge: %w", label, owner, name, pr.Number, err)
		}
		merged++
		_, _ = fmt.Fprintf(r.Runner.Stderr, "%s: merged %s/%s#%d (issue #%d, head %s)\n", label, owner, name, pr.Number, linked, mergedHead)
	}
	return merged, skipped, nil
}

func mergeDirectorPullRequest(ctx context.Context, cl *forgejoClient, owner, repo string, index int, head, mergeStyle string, status *directorMergeStatusCheck) (string, error) {
	mergedHead, err := mergePullRequestWithHeadAndStyleSettled(ctx, cl, owner, repo, index, head, mergeStyle, status)
	if err != nil {
		return "", err
	}
	if err := requirePullRequestMerged(ctx, cl, owner, repo, index, mergedHead); err != nil {
		var postErr *prMergePostconditionError
		if errors.As(err, &postErr) && postErr.closedButUnmerged() {
			return recoverClosedUnmergedDirectorMerge(ctx, cl, owner, repo, index, mergeStyle, postErr)
		}
		return "", err
	}
	return mergedHead, nil
}

func recoverClosedUnmergedDirectorMerge(ctx context.Context, cl *forgejoClient, owner, repo string, index int, mergeStyle string, postErr *prMergePostconditionError) (string, error) {
	if postErr == nil || !postErr.closedButUnmerged() {
		return "", fmt.Errorf("merge postcondition failed for %s/%s#%d: %w", owner, repo, index, postErr)
	}
	pr, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", fmt.Errorf("%w; could not refresh closed PR before recovery: %w", postErr, err)
	}
	head, err := closedUnmergedRecoveryTarget(pr, postErr)
	if err != nil {
		return "", err
	}
	status, reason, ok := directorMergeStatusGate(ctx, cl, owner, repo, pr.Base.Ref, head)
	if !ok {
		return "", fmt.Errorf("%w; closed PR is no longer eligible for retry: %s", postErr, reason)
	}
	return retryClosedUnmergedDirectorMerge(ctx, cl, owner, repo, index, head, mergeStyle, status, postErr)
}

func closedUnmergedRecoveryTarget(pr *forgejoPullRequest, postErr *prMergePostconditionError) (head string, err error) {
	state := strings.ToLower(strings.TrimSpace(pr.State))
	if state != "closed" {
		return "", fmt.Errorf("%w; expected closed PR before recovery, got state %s", postErr, emptyDefault(state, "unknown"))
	}
	head = pr.HeadSHA()
	if head == "" {
		return "", fmt.Errorf("%w; closed PR no longer exposed a head SHA", postErr)
	}
	if want := strings.TrimSpace(postErr.HeadSHA); want != "" && !strings.EqualFold(head, want) {
		return "", fmt.Errorf("%w; closed PR head changed from %s to %s before recovery", postErr, want, head)
	}
	return head, nil
}

func retryClosedUnmergedDirectorMerge(ctx context.Context, cl *forgejoClient, owner, repo string, index int, head, mergeStyle string, status directorMergeStatusCheck, postErr *prMergePostconditionError) (string, error) {
	if err := cl.ReopenIssue(ctx, owner, repo, index); err != nil {
		return "", fmt.Errorf("%w; reopen before retry failed: %w", postErr, err)
	}
	reopened, err := cl.GetPullRequest(ctx, owner, repo, index)
	if err != nil {
		return "", fmt.Errorf("%w; reopened PR refresh failed: %w", postErr, err)
	}
	if err := validateReopenedDirectorMergeCandidate(reopened, head, postErr); err != nil {
		return "", err
	}
	recoveredHead, retryErr := mergePullRequestWithHeadAndStyleSettled(ctx, cl, owner, repo, index, head, mergeStyle, &status)
	if retryErr != nil {
		return "", fmt.Errorf("%w; retry after reopen failed: %w", postErr, retryErr)
	}
	if err := requirePullRequestMerged(ctx, cl, owner, repo, index, recoveredHead); err != nil {
		return "", fmt.Errorf("%w; retry after reopen still did not prove merged: %w", postErr, err)
	}
	return recoveredHead, nil
}

func validateReopenedDirectorMergeCandidate(pr *forgejoPullRequest, expectedHead string, postErr *prMergePostconditionError) error {
	reopenedState := strings.ToLower(strings.TrimSpace(pr.State))
	if reopenedState != "open" {
		return fmt.Errorf("%w; reopen did not restore open state, got %s", postErr, emptyDefault(reopenedState, "unknown"))
	}
	reopenedHead := strings.TrimSpace(pr.HeadSHA())
	if reopenedHead == "" {
		return fmt.Errorf("%w; reopened PR lost its head SHA", postErr)
	}
	if !strings.EqualFold(reopenedHead, expectedHead) {
		return fmt.Errorf("%w; reopened PR head changed from %s to %s", postErr, expectedHead, reopenedHead)
	}
	return nil
}

// directorMergeEligibility returns whether pr is the narrow, ward-owned lane.
// The policy closes over the issue thread, not just the PR title.
func directorMergeEligibility(ctx context.Context, owner, repo string, pr directorPullRequest, prClient *forgejoClient, issueClient Tracker) (ok bool, reason string, linked int, meta directorRunMeta) {
	linked, ok = directorLinkedIssueNumber(pr.Body)
	if !ok {
		return false, "no same-repo closing reference in the PR body", 0, directorRunMeta{}
	}
	if pr.MergeableKnown && !pr.Mergeable {
		return false, directorMergeConflictReason(ctx, owner, repo, linked, pr, issueClient), linked, directorRunMeta{}
	}
	meta, reason, allowed := directorMergeIssueMeta(ctx, owner, repo, pr, linked, prClient, issueClient)
	if !allowed {
		return false, reason, linked, meta
	}
	return directorMergeDecision(pr.Issue, linked, meta)
}

func directorMergeConflictReason(ctx context.Context, owner, repo string, linked int, pr directorPullRequest, issueClient Tracker) string {
	comments, err := issueClient.ListIssueComments(ctx, owner, repo, linked)
	if err != nil {
		return "PR is not mergeable against the current base branch; rebase or merge base and resolve the conflict first"
	}
	return directorMergeConflictReasonFromComments(pr, comments, time.Now().UTC())
}

func directorMergeConflictReasonFromComments(pr directorPullRequest, comments []issueComment, now time.Time) string {
	latest, ok := latestBacklogOutcomeComment(comments)
	if !ok {
		if pr.UpdatedAt.IsZero() {
			return "PR is not mergeable against the current base branch; active worker branch with no WARDED_WORKFLOW yet"
		}
		age := now.Sub(pr.UpdatedAt)
		if age >= directorMergeConflictStaleAfter {
			return fmt.Sprintf("PR is not mergeable against the current base branch; stale worker branch with no WARDED_WORKFLOW yet (updated %s ago)", humanDuration(age))
		}
		return fmt.Sprintf("PR is not mergeable against the current base branch; active worker branch with no WARDED_WORKFLOW yet (updated %s ago)", humanDuration(age))
	}
	meta := parseDirectorRunMeta(latest.Body)
	switch strings.ToLower(strings.TrimSpace(meta.Outcome.Status)) {
	case "blocked", "failed":
		review := strings.TrimSpace(meta.Review)
		if review == "" {
			return fmt.Sprintf("PR is not mergeable against the current base branch; linked issue is %s", meta.Outcome.Status)
		}
		return fmt.Sprintf("PR is not mergeable against the current base branch; linked issue is %s (%s)", meta.Outcome.Status, review)
	case "submitted":
		return "PR is not mergeable against the current base branch; linked issue is submitted, not merge-ready"
	case "merge-ready":
		return "PR is not mergeable against the current base branch; linked issue is merge-ready but the branch still conflicts with main"
	default:
		return fmt.Sprintf("PR is not mergeable against the current base branch; linked issue ended with %s", workflowOutcomeVisible(meta.Outcome.Status))
	}
}

func directorMergeIssueMeta(ctx context.Context, owner, repo string, pr directorPullRequest, linked int, prClient *forgejoClient, issueClient Tracker) (directorRunMeta, string, bool) {
	var meta directorRunMeta
	if reason, ok := directorMergePullRequestGate(pr); !ok {
		return directorRunMeta{}, reason, false
	}
	if _, err := issueClient.GetIssue(ctx, owner, repo, linked); err != nil {
		return directorRunMeta{}, "could not read linked issue: " + firstLine(err.Error()), false
	}
	comments, err := issueClient.ListIssueComments(ctx, owner, repo, linked)
	if err != nil {
		return directorRunMeta{}, "could not read linked issue comments: " + firstLine(err.Error()), false
	}
	prInfo, err := prClient.GetPullRequest(ctx, owner, repo, pr.Number)
	if err != nil {
		return directorRunMeta{}, "could not read linked PR details: " + firstLine(err.Error()), false
	}
	meta.IssueRef = fmt.Sprintf("%s/%s#%d", owner, repo, linked)
	meta.PRHeadSHA = strings.TrimSpace(prInfo.Head.SHA)
	meta.PRRef = prInfo.Ref(owner, repo)
	if meta.PRHeadSHA == "" {
		return meta, "linked PR did not expose a head SHA", false
	}
	status, reason, ok := directorMergeStatusGate(ctx, prClient, owner, repo, prInfo.Base.Ref, meta.PRHeadSHA)
	if !ok {
		return directorRunMeta{Status: status.Status}, reason, false
	}
	latest, ok := latestBacklogOutcomeComment(comments)
	if !ok {
		return directorRunMeta{}, "linked issue never reached a WARDED_WORKFLOW comment", false
	}
	meta = parseDirectorRunMeta(latest.Body)
	meta.CommentedBy = latest.User.Login
	meta.CommentedAt = latest.CreatedAt
	meta.IssueRef = fmt.Sprintf("%s/%s#%d", owner, repo, linked)
	meta.PRHeadSHA = strings.TrimSpace(prInfo.Head.SHA)
	meta.PRRef = prInfo.Ref(owner, repo)
	meta.Status = status.Status
	qa, ok := latestQAVerdictComment(comments, meta.IssueRef, meta.PRRef, meta.PRHeadSHA)
	if !ok {
		return meta, "linked issue does not have a passing WARDED_WORKFLOW QA verdict for the current PR head SHA", false
	}
	meta.QA = qa
	return meta, "", true
}

func directorMergePullRequestGate(pr directorPullRequest) (string, bool) {
	switch {
	case !pr.MergeableKnown:
		if pr.MergeableError == "" {
			return "could not read PR mergeability", false
		}
		return "could not read PR mergeability: " + pr.MergeableError, false
	case !pr.Mergeable:
		return "PR is not mergeable against the current base branch; rebase or merge base and resolve the conflict first", false
	}
	wf, ok := directorPRWorkflowMarker(pr.Body)
	if !ok {
		return "PR body missing ward.workflow: pull-request-and-merge marker", false
	}
	if wf != string(workflowPullRequestAndMerge) {
		return "PR body carries ward.workflow: " + wf + "; need pull-request-and-merge", false
	}
	return "", true
}

// directorMergeStatusGate checks the live branch-protection requirement set
// against the PR head SHA immediately before merge.
func directorMergeStatusGate(ctx context.Context, cl *forgejoClient, owner, repo, baseBranch, headSHA string) (directorMergeStatusCheck, string, bool) {
	baseBranch = strings.TrimSpace(baseBranch)
	headSHA = strings.TrimSpace(headSHA)
	if baseBranch == "" {
		return directorMergeStatusCheck{}, "linked PR did not expose a base branch", false
	}
	branch, err := cl.GetBranch(ctx, owner, repo, baseBranch)
	if err != nil {
		return directorMergeStatusCheck{}, "could not read base branch status checks: " + firstLine(err.Error()), false
	}
	required := normalizeDirectorRequiredContexts(branch.StatusCheckContexts)
	combined, err := cl.GetCommitCombinedStatus(ctx, owner, repo, headSHA)
	if err != nil {
		return directorMergeStatusCheck{}, "could not read live commit status for current PR head SHA: " + firstLine(err.Error()), false
	}
	summary, reason, ok := buildDirectorMergeStatusSummary(headSHA, baseBranch, required, combined)
	if !ok {
		return directorMergeStatusCheck{Status: summary}, reason, false
	}
	return directorMergeStatusCheck{Status: summary}, "", true
}

type directorMergeStatusCheck struct {
	Status directorMergeStatusSummary
}

type directorMergeStatusSummary struct {
	Branch  string
	HeadSHA string
	State   string
	Checks  []directorMergeStatusContext
}

type directorMergeStatusContext struct {
	Context string
	State   string
}

func (s directorMergeStatusSummary) summary() string {
	if s.HeadSHA == "" && s.State == "" && len(s.Checks) == 0 {
		return "<no status>"
	}
	parts := s.contextParts()
	if len(parts) == 0 {
		return fmt.Sprintf("%s on %s", s.State, s.HeadSHA)
	}
	return fmt.Sprintf("%s on %s (%s)", s.State, s.HeadSHA, strings.Join(parts, ", "))
}

func (s directorMergeStatusSummary) contextSummary() string {
	parts := s.contextParts()
	if len(parts) == 0 {
		return "<status unavailable>"
	}
	return strings.Join(parts, ", ")
}

func (s directorMergeStatusSummary) contextParts() []string {
	var parts []string
	for _, check := range s.Checks {
		parts = append(parts, fmt.Sprintf("%s=%s", check.Context, check.State))
	}
	return parts
}

func normalizeDirectorRequiredContexts(contexts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range contexts {
		if c = strings.TrimSpace(c); c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func buildDirectorMergeStatusSummary(headSHA, branch string, required []string, combined *forgejoCommitCombinedStatus) (directorMergeStatusSummary, string, bool) {
	summary := directorMergeStatusSummary{Branch: branch, HeadSHA: headSHA, State: strings.ToLower(strings.TrimSpace(combined.State))}
	if summary.State == "" {
		summary.State = "unknown"
	}
	statusByContext := map[string]string{}
	var fallback []string
	for _, st := range combined.Statuses {
		ctx := strings.TrimSpace(st.Context)
		if ctx == "" {
			continue
		}
		state := strings.ToLower(st.EffectiveState())
		if state == "" {
			continue
		}
		if _, seen := statusByContext[ctx]; !seen {
			fallback = append(fallback, ctx)
		}
		statusByContext[ctx] = state
	}
	if len(required) == 0 {
		required = fallback
		if len(required) == 0 {
			return summary, "base branch does not declare any required status contexts", false
		}
	}
	for _, ctx := range required {
		state, ok := statusByContext[ctx]
		if !ok {
			return summary, fmt.Sprintf("linked PR head SHA %s is missing required status context %s", headSHA, ctx), false
		}
		if state != "success" {
			return summary, fmt.Sprintf("linked PR head SHA %s has required status context %s=%s", headSHA, ctx, state), false
		}
		summary.Checks = append(summary.Checks, directorMergeStatusContext{Context: ctx, State: state})
	}
	if summary.State != "success" {
		return summary, fmt.Sprintf("linked PR head SHA %s did not report a successful combined status", headSHA), false
	}
	return summary, "", true
}

// directorPRWorkflowMarker extracts the workflow marker from a PR body.
func directorPRWorkflowMarker(body string) (string, bool) {
	for _, ln := range strings.Split(body, "\n") {
		s := backlogCommentLine(ln)
		if s == "" {
			continue
		}
		if m := directorWorkflowMarkerRE.FindStringSubmatch(s); m != nil {
			return string(canonicalWorkflow(workflowMode(strings.ToLower(strings.TrimSpace(m[1]))))), true
		}
	}
	return "", false
}

// directorMergeDecision is the pure policy boundary for the director merge lane.
func directorMergeDecision(pr Issue, linked int, meta directorRunMeta) (ok bool, reason string, _ int, _ directorRunMeta) {
	title := strings.ToLower(strings.TrimSpace(pr.Title))
	switch {
	case strings.HasPrefix(title, "ward salvage:"):
		return false, "salvage PRs are cleanup noise, not merge-authorized work", linked, meta
	case strings.HasPrefix(title, "wip:") || strings.HasPrefix(title, "[wip]"):
		return false, "draft PRs are not merge-authorized", linked, meta
	}
	status := strings.ToLower(strings.TrimSpace(meta.Outcome.Status))
	ready := status == "merge-ready"
	if !ready && status == "submitted" {
		ready = strings.EqualFold(strings.TrimSpace(meta.MergeAuthorization), "reviewed-and-ready") || strings.EqualFold(strings.TrimSpace(meta.MergeAuthorization), "merge-ready")
	}
	if !ready {
		if !meta.HasOutcome {
			return false, "linked issue did not finish with a WARDED_WORKFLOW outcome comment", linked, meta
		}
		return false, "linked issue did not finish with " + workflowOutcomeVisible("merge-ready"), linked, meta
	}
	wf := strings.TrimSpace(meta.Workflow)
	if wf != string(workflowPullRequestAndMerge) {
		if wf == "" {
			return false, "linked issue comment did not record the merge workflow", linked, meta
		}
		return false, "workflow " + meta.Workflow + " still needs human merge approval", linked, meta
	}
	if reason, ok := directorMergeQAGate(meta); !ok {
		return false, reason, linked, meta
	}
	return true, "", linked, meta
}

func directorMergeQAGate(meta directorRunMeta) (reason string, ok bool) {
	if strings.TrimSpace(meta.QA.ReviewerFamily) != qaFamilyInternal {
		return "linked issue QA verdict was not from the internal reviewer family", false
	}
	if strings.ToLower(strings.TrimSpace(meta.QA.Verdict)) != "pass" {
		return "linked issue QA verdict did not pass", false
	}
	if strings.TrimSpace(meta.QA.ReviewedSHA) != strings.TrimSpace(meta.PRHeadSHA) {
		return "linked issue QA verdict does not match the current PR head SHA", false
	}
	if strings.TrimSpace(meta.QA.IssueRef) != strings.TrimSpace(meta.IssueRef) {
		return "linked issue QA verdict does not name the current issue", false
	}
	if strings.TrimSpace(meta.QA.PRRef) != strings.TrimSpace(meta.PRRef) {
		return "linked issue QA verdict does not name the current PR", false
	}
	if strings.TrimSpace(meta.QA.RunIdentity) == "" {
		return "linked issue QA verdict is missing run identity", false
	}
	return "", true
}

// recordDirectorMergeDone posts the director's final done outcome only after the
// PR has actually merged to main.
func recordDirectorMergeDone(ctx context.Context, cl Tracker, owner, repo string, linked, prNumber int, meta directorRunMeta) error {
	body := directorMergeDoneComment(prNumber, meta)
	return cl.CommentIssue(ctx, owner, repo, linked, body)
}

func directorMergeDoneComment(prNumber int, meta directorRunMeta) string {
	workflow := strings.TrimSpace(meta.Workflow)
	if workflow == "" {
		workflow = workflowMachineToken(workflowPullRequestAndMerge)
	} else {
		workflow = workflowMachineToken(workflowMode(workflow))
	}
	review := strings.TrimSpace(meta.Review)
	if review == "" {
		review = "passed: director merged the pull request"
	}
	status := strings.TrimSpace(meta.Status.contextSummary())
	if status == "" {
		status = "<status unavailable>"
	}
	return fmt.Sprintf(
		"WARDED_WORKFLOW: done ✅\n\n"+
			"<details><summary>details</summary>\n\n"+
			"workflow: %s; review summary: %s\n\n"+
			"checked head sha: %s\n"+
			"status context: %s\n"+
			"status state: %s\n\n"+
			"merged PR #%d to main after the merge gate passed.\n\n"+
			"</details>",
		workflow, review, strings.TrimSpace(meta.Status.HeadSHA), status, strings.TrimSpace(meta.Status.State), prNumber)
}

// directorLinkedIssueNumber extracts the first same-repo closing reference from a
// PR body. It is the join key from the PR back to the carried issue thread.
func directorLinkedIssueNumber(body string) (int, bool) {
	m := directorClosingRefRE.FindStringSubmatch(body)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
