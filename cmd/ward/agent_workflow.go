package main

// agent_workflow.go carries the workflow-mode axis (ward#508):
// merge-remote-main, pull-request, pull-request-and-merge, remote-branch-only.

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// workflowMode is the landing policy for a run.
// The zero value reads as merge-remote-main unless smart defaults override it.
type workflowMode string

const (
	// workflowDirectToMain is the fast path: carry the issue through commit,
	// merge to main, push, and close. The trusted-repo default (ward#508).
	workflowDirectToMain workflowMode = "merge-remote-main"
	// workflowPullRequest carries the work to a branch + pull request instead of
	// landing on main directly; the PR boundary is the merge gate.
	workflowPullRequest workflowMode = "pull-request"
	// workflowPullRequestAndMerge is the explicit director-merge lane.
	workflowPullRequestAndMerge workflowMode = "pull-request-and-merge"
	// workflowRemoteBranchOnly pushes a remote branch and stops there: no PR, no merge.
	workflowRemoteBranchOnly workflowMode = "remote-branch-only"

	// defaultWorkflow is the mode a run takes when --workflow and smart defaults
	// leave it unset. merge-remote-main is the baseline; defaults can override it.
	defaultWorkflow = workflowDirectToMain

	// directorMergeWorkflowMarker is the PR-body marker the director sweep reads
	// when deciding whether a ward-owned PR may be merged automatically.
	directorMergeWorkflowMarker      = "ward.workflow: pull-request-and-merge"
	pullRequestWorkflowOutcomeMarker = wardWorkflowMarker + " <fully-qualified pull request link>"
)

func workflowOutcomeLinkMarker(f forge) string {
	noun := workflowReviewNoun(f)
	return workflowCommentVisible("<fully-qualified " + noun + " link>")
}

// orDefault collapses the "" zero value onto the default and normalizes legacy
// aliases so every helper can read a concrete mode without each caller re-checking.
func (w workflowMode) orDefault() workflowMode {
	if w == "" {
		return defaultWorkflow
	}
	return canonicalWorkflow(w)
}

// landsOnMain reports whether this workflow may push/merge to main.
func (w workflowMode) landsOnMain() bool {
	return w.orDefault() == workflowDirectToMain
}

// workflowChoices renders the supported --workflow values as a pipe list for flag
// usage and error text, mirroring agentHarnessChoices.
func workflowChoices() string {
	return strings.Join([]string{
		string(workflowDirectToMain), string(workflowPullRequest),
		string(workflowPullRequestAndMerge), string(workflowRemoteBranchOnly),
	}, "|")
}

// parseWorkflow resolves a --workflow string to a workflowMode, treating empty as
// the default and erroring on an unknown mode with a --workflow-shaped message.
func parseWorkflow(s string) (workflowMode, error) {
	switch raw := strings.TrimSpace(s); raw {
	case "":
		return defaultWorkflow, nil
	case string(workflowDirectToMain):
		return workflowDirectToMain, nil
	case "direct-main", "direct-to-main":
		return warnWorkflowAlias(raw, workflowDirectToMain)
	case string(workflowPullRequest):
		return workflowPullRequest, nil
	case "pull-requests":
		return warnWorkflowAlias(raw, workflowPullRequest)
	case string(workflowPullRequestAndMerge):
		return workflowPullRequestAndMerge, nil
	case "pull-requests-and-merge":
		return warnWorkflowAlias(raw, workflowPullRequestAndMerge)
	case string(workflowRemoteBranchOnly):
		return workflowRemoteBranchOnly, nil
	case "patch-only":
		return warnWorkflowAlias(raw, workflowRemoteBranchOnly)
	default:
		return "", fmt.Errorf("invalid --workflow %q: want %s", s, workflowChoices())
	}
}

// workflowFlag is the visible --workflow selector for detached engineer runs.
// It defaults to merge-remote-main unless smart defaults override it.
func workflowFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "workflow",
		Value: string(defaultWorkflow),
		Usage: "landing policy for the run: " + workflowChoices() + " (default merge-remote-main unless smart defaults override). " +
			"merge-remote-main merges to main and closes; pull-request opens a pull request and stops there; pull-request-and-merge opens a pull request and marks it director-merge eligible; remote-branch-only pushes a remote branch and lands nothing else. " +
			"Deprecated aliases direct-main/direct-to-main/pull-requests/pull-requests-and-merge/patch-only are still accepted with warnings. The removed pr short form is rejected.",
	}
}

// warnWorkflowAlias keeps transitional spellings readable during the rename.
func warnWorkflowAlias(raw string, canonical workflowMode) (workflowMode, error) {
	fmt.Fprintf(os.Stderr, "ward: --workflow %s is deprecated; use %s\n", raw, canonical)
	return canonical, nil
}

// canonicalWorkflow normalizes legacy spellings into the canonical workflow names.
func canonicalWorkflow(w workflowMode) workflowMode {
	switch strings.TrimSpace(string(w)) {
	case "":
		return defaultWorkflow
	case string(workflowDirectToMain), "direct-main", "direct-to-main":
		return workflowDirectToMain
	case string(workflowPullRequest), "pull-requests":
		return workflowPullRequest
	case string(workflowPullRequestAndMerge), "pull-requests-and-merge":
		return workflowPullRequestAndMerge
	case string(workflowRemoteBranchOnly), "patch-only":
		return workflowRemoteBranchOnly
	default:
		return workflowMode(strings.TrimSpace(string(w)))
	}
}

// workflowMachineToken renders the canonical workflow token
// for machine-readable comment bodies and PR markers.
func workflowMachineToken(w workflowMode) string {
	if canonicalWorkflow(w) == workflowPullRequestAndMerge {
		return "pull-request-and-merge"
	}
	return string(canonicalWorkflow(w))
}

// agentWorkflow resolves the --workflow flag to a workflowMode, erroring on an
// unknown value. CLI wins; otherwise smart defaults resolve by target repo.
func agentWorkflow(c *cli.Command, repoSlug string) (workflowMode, error) {
	if c.IsSet("workflow") {
		return parseWorkflow(c.String("workflow"))
	}
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		return "", err
	}
	if wf, ok := defs.agentWorkflowRepos[strings.TrimSpace(repoSlug)]; ok {
		return wf, nil
	}
	return defs.agentWorkflowDefault.orDefault(), nil
}

// workflowCarryClause returns the carry clause for the selected workflow.
func workflowCarryClause(ref agentIssueRef, wf workflowMode) string {
	switch wf.orDefault() {
	case workflowDirectToMain:
		return directToMainCarryClause(ref)
	case workflowPullRequest:
		return pullRequestCarryClause(ref)
	case workflowPullRequestAndMerge:
		return pullRequestAndMergeCarryClause(ref)
	case workflowRemoteBranchOnly:
		return remoteBranchOnlyCarryClause(ref)
	default:
		return directToMainCarryClause(ref)
	}
}

func workflowReviewNoun(f forge) string {
	if f == forgeGitLab {
		return "merge request"
	}
	return "pull request"
}

// directToMainCarryClause tells the agent to land the run on main.
func directToMainCarryClause(ref agentIssueRef) string {
	return fmt.Sprintf(
		"implement, commit, merge to main, push - and close the issue with a commit trailer: %s.",
		ref.closingTrailer())
}

// pullRequestCarryClause tells the agent to land via a pull request.
func pullRequestCarryClause(ref agentIssueRef) string {
	noun := workflowReviewNoun(ref.Forge)
	return fmt.Sprintf(
		"implement on a feature branch, commit, push the branch to origin, and open a %s "+
			"against `main` whose body carries `%s`. "+
			"%s "+
			"%s Do NOT push to `main` directly or merge it yourself - in the `pull-request` workflow the %s "+
			"IS the merge gate, and the director is encouraged to merge it later if policy allows. When the %s is green, "+
			"the engineer's final visible outcome is `%s`, not `done`.",
		noun, ref.closingTrailer(), pullRequestDescriptionClause(noun), pullRequestCIWatchClauseFor(ref.Forge), noun, noun, workflowOutcomeLinkMarker(ref.Forge))
}

// pullRequestAndMergeCarryClause tells the agent to open a PR and keep it merge-ready
// until the PR is actually merged.
func pullRequestAndMergeCarryClause(ref agentIssueRef) string {
	noun := workflowReviewNoun(ref.Forge)
	return fmt.Sprintf(
		"implement on a feature branch, commit, push the branch to origin, and open a %s "+
			"against `main` whose body carries `%s` and `%s`. This run is director-merge authorized: "+
			"%s "+
			"the worker still opens the %s, but the engineer's final visible outcome is `%s`; "+
			"the run is not done until the %s is merged and the director records the final done outcome. "+
			"%s Keep the branch ready for merge and do not claim success early.",
		noun, ref.closingTrailer(), directorMergeWorkflowMarker, pullRequestDescriptionClause(noun), noun, workflowOutcomeLinkMarker(ref.Forge), noun, pullRequestCIWatchClauseFor(ref.Forge))
}

// pullRequestDescriptionClause tells the worker to write a short useful PR body.
func pullRequestDescriptionClause(noun string) string {
	return fmt.Sprintf(
		"Write a short %s description before opening it. Keep it to a paragraph or two and a small bullet list: what changed, why, and any test or CI notes. Do not leave the %s body empty.",
		noun, noun)
}

// pullRequestCIWatchClause tells pull-request workflows that opening the PR is not
// the end. They must keep watching CI/checks and only report done once the PR is green.
func pullRequestCIWatchClauseFor(f forge) string {
	noun := workflowReviewNoun(f)
	return "After the " + noun + " opens, keep watching its CI/checks and fetch the status/logs if anything " +
		"fails. Patch the branch, push updates, and repeat until the checks are green or the failure is " +
		"genuinely blocked. A failing check is not a done state, human feedback on the issue or " + noun + " is " +
		"blocking until it is visibly acknowledged, and the final `WARD-WORKFLOW` comment is not allowed until " +
		"the " + noun + " is green. " + workflowFailureCommentClauseFor(f)
}

// workflowFailureCommentClause tells PR workflows to mirror failure comments onto
// the PR itself, while keeping the issue comment unchanged.
func workflowFailureCommentClauseFor(f forge) string {
	if f == forgeGitLab {
		noun := workflowReviewNoun(f)
		return "If this run has opened or found a " + noun + " and then fails for any reason, post the same " +
			"actionable failure comment to both the linked issue and the " + noun + ". Keep the issue comment unchanged, " +
			"including any reservation-lock release/clear/hand-back wording that belongs there. The " + noun + " comment " +
			"must omit reservation-lock release/clear/hand-back wording and should reuse the existing " +
			"signature/idempotency marker if one is present. If no " + noun + " exists, skip the " + noun + " comment."
	}
	return "If this run has opened or found a PR and then fails for any reason, post the same actionable failure " +
		"comment to both the linked issue and the PR. Keep the issue comment unchanged, including any " +
		"reservation-lock release/clear/hand-back wording that belongs there. The PR comment must omit " +
		"reservation-lock release/clear/hand-back wording and should reuse the existing signature/idempotency marker " +
		"if one is present. If no PR exists, skip the PR comment."
}

// remoteBranchOnlyCarryClause tells the agent it has no PR or merge authority:
// push the branch and stop there.
func remoteBranchOnlyCarryClause(ref agentIssueRef) string {
	return fmt.Sprintf(
		"implement on a feature branch, commit, and push the branch to origin. This `remote-branch-only` "+
			"workflow has no pull requests or merge authority. do not open a pull request, do not merge to `main`, "+
			"and do not write a `%s` trailer - the remote branch is the only landing surface here.",
		ref.closingTrailer())
}

// workflowLandingPhrase names "done" for the reflection's "only after ..." opener.
func workflowLandingPhrase(ref agentIssueRef, wf workflowMode) string {
	noun := workflowReviewNoun(ref.Forge)
	switch wf.orDefault() {
	case workflowDirectToMain:
		return "the work is committed, merged to main, and pushed"
	case workflowPullRequest:
		return "the branch is pushed, the " + noun + " is open, and the required checks are green"
	case workflowPullRequestAndMerge:
		return "the " + noun + " is reviewed and merge-ready"
	case workflowRemoteBranchOnly:
		return "the remote branch is pushed"
	default:
		return "the work is committed, merged to main, and pushed"
	}
}

// workflowOutcomeStatus names the nonterminal completion state a worker should
// report at its boundary for the selected workflow.
func workflowOutcomeStatus(wf workflowMode, reviewGate bool) string {
	switch mode := string(canonicalWorkflow(wf.orDefault())); mode {
	case string(workflowPullRequest):
		return "submitted"
	case string(workflowPullRequestAndMerge):
		if !reviewGate {
			return "submitted"
		}
		return "merge-ready"
	case string(workflowDirectToMain):
		return "done"
	case string(workflowRemoteBranchOnly):
		return "done"
	default:
		return "done"
	}
}
