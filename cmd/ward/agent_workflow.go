package main

// agent_workflow.go carries the workflow-mode axis (ward#508): direct-main,
// pull-requests, pull-requests-and-merge, or patch-only.

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// workflowMode is the landing policy for a run. The zero value ("") reads as the
// default, so a plan built without the flag follows the configured safe gate.
type workflowMode string

const (
	// workflowDirectMain is today's fast path: carry the issue through commit, merge
	// to main, push, and close. The trusted-repo default (ward#508).
	workflowDirectMain workflowMode = "direct-main"
	// workflowPullRequests carries the work to a branch + pull request instead of
	// landing on main directly; a human is the merge gate.
	workflowPullRequests workflowMode = "pull-requests"
	// workflowPullRequestsAndMerge carries the work to a branch + pull request and
	// then merges it with a merge commit after CI and review gate pass.
	workflowPullRequestsAndMerge workflowMode = "pull-requests-and-merge"
	// workflowPatchOnly produces a patch/diff and reports it in a comment with NO
	// landing authority: it neither pushes main nor opens a PR (untrusted targets).
	workflowPatchOnly workflowMode = "patch-only"

	// Compatibility alias for older code/config. The spelled-out names are the
	// primary vocabulary now.
	workflowPR = workflowPullRequests

	// defaultWorkflow is the mode a run takes when --workflow and smart defaults
	// leave it unset. direct-main is the baked fleet default.
	defaultWorkflow = workflowDirectMain
)

// orDefault collapses the "" zero value onto the default.
func (w workflowMode) orDefault() workflowMode {
	if w == "" {
		return defaultWorkflow
	}
	return w
}

// landsOnMain reports whether this workflow may push/merge to main.
// Only direct-main does (ward#508).
func (w workflowMode) landsOnMain() bool {
	return w.orDefault() == workflowDirectMain
}

// workflowUsesReviewGate reports whether the headless seed should wire in the
// in-container review gate before landing.
func (w workflowMode) workflowUsesReviewGate() bool {
	switch w.orDefault() {
	case workflowDirectMain, workflowPullRequestsAndMerge:
		return true
	default:
		return false
	}
}

// workflowChoices renders the supported --workflow values as a pipe list for flag
// usage and error text, mirroring agentHarnessChoices.
func workflowChoices() string {
	return strings.Join([]string{
		string(workflowDirectMain), string(workflowPullRequests), string(workflowPullRequestsAndMerge), string(workflowPatchOnly),
	}, "|")
}

// parseWorkflow resolves a --workflow string to a workflowMode, treating empty as
// the default and erroring on an unknown mode with a --workflow-shaped message.
func parseWorkflow(s string) (workflowMode, error) {
	switch strings.TrimSpace(s) {
	case "":
		return defaultWorkflow, nil
	case string(workflowDirectMain):
		return workflowDirectMain, nil
	case string(workflowPullRequests), "pr":
		return workflowPullRequests, nil
	case string(workflowPullRequestsAndMerge):
		return workflowPullRequestsAndMerge, nil
	case string(workflowPatchOnly):
		return workflowPatchOnly, nil
	default:
		return "", fmt.Errorf("invalid --workflow %q: want %s", s, workflowChoices())
	}
}

// workflowFlag is the visible --workflow selector shared by the detached engineer
// surfaces (a bare ref, `engineer`, freeform). Defaults to the baked fleet default.
func workflowFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "workflow",
		Value: string(defaultWorkflow),
		Usage: "landing policy for the run: " + workflowChoices() + " (default direct-main unless smart defaults override). " +
			"direct-main merges to main; pull-requests opens a pull request and stops there; pull-requests-and-merge opens a pull request, waits for green checks and review, then merges with a merge commit; patch-only reports a patch and lands nothing.",
	}
}

// agentWorkflow resolves the --workflow flag to a workflowMode, erroring on an
// unknown value. CLI wins; otherwise smart defaults resolve by target repo.
func agentWorkflow(c *cli.Command, repoSlug string) (workflowMode, error) {
	if c.IsSet("workflow") {
		raw := strings.TrimSpace(c.String("workflow"))
		wf, err := parseWorkflow(raw)
		if err != nil {
			return "", err
		}
		if raw == "pr" {
			fmt.Fprintln(os.Stderr, "ward agent: WARNING: --workflow pr is deprecated; use --workflow pull-requests instead")
		}
		return wf, nil
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

// workflowCarryClause is the workflow-specific tail of the seed's carry sentence.
// direct-main defers to the forge clause; the PR modes override it.
func workflowCarryClause(ref agentIssueRef, wf workflowMode) string {
	switch wf.orDefault() {
	case workflowDirectMain:
		return forgeCarryClause(ref)
	case workflowPullRequests:
		return pullRequestsWorkflowCarryClause(ref)
	case workflowPullRequestsAndMerge:
		return pullRequestsAndMergeWorkflowCarryClause(ref)
	case workflowPatchOnly:
		return patchOnlyCarryClause(ref)
	default:
		return forgeCarryClause(ref)
	}
}

// pullRequestsWorkflowCarryClause tells the agent to land via a pull request,
// never a direct main push.
func pullRequestsWorkflowCarryClause(ref agentIssueRef) string {
	if ref.Forge == forgeGitHub {
		// GitHub's forge clause is already branch + PR (main is protected), so reuse it.
		return forgeCarryClause(ref)
	}
	return fmt.Sprintf(
		"implement on a feature branch, commit, push the branch to origin, and open a pull request "+
			"against `main` whose body carries `closes #%d`. Do NOT push to `main` directly or merge it "+
			"yourself - in the `pull-requests` workflow the pull request IS the done-condition, landed by a "+
			"human or a follow-up loop, not by you.",
		ref.Number)
}

// pullRequestsAndMergeWorkflowCarryClause tells the agent to open a pull request
// first, then finish the lane with a merge commit after CI + review.
func pullRequestsAndMergeWorkflowCarryClause(ref agentIssueRef) string {
	if ref.Forge == forgeGitHub {
		return fmt.Sprintf(
			"implement on a feature branch, commit, push the branch to origin, and open a pull request "+
				"with `gh pr create` whose body carries `Closes #%d`. After the pull request is green and "+
				"passes the review gate, merge it with a merge commit.",
			ref.Number)
	}
	return fmt.Sprintf(
		"implement on a feature branch, commit, push the branch to origin, and open a pull request "+
			"against `main` whose body carries `closes #%d`. After CI is green and the review gate passes, "+
			"merge it with a merge commit.",
		ref.Number)
}

// patchOnlyCarryClause tells the agent it has no landing authority: commit locally,
// but produce a patch and report it in a comment rather than pushing or merging.
func patchOnlyCarryClause(ref agentIssueRef) string {
	return fmt.Sprintf(
		"implement on a feature branch and commit, but do NOT push and do NOT merge to `main` - in the "+
			"`patch-only` workflow this run has no landing authority. Instead, capture your change as a patch "+
			"(`git format-patch origin/main --stdout` or `git diff origin/main`) and post it in a comment on "+
			"issue #%d for a human to review and apply. Do not write a `closes #%d` trailer - landing the work "+
			"is not yours to do in this workflow.",
		ref.Number, ref.Number)
}

// workflowLandingPhrase names "done" for the reflection's "only after ..." opener.
func workflowLandingPhrase(ref agentIssueRef, wf workflowMode) string {
	switch wf.orDefault() {
	case workflowDirectMain:
		if ref.Forge == forgeGitHub {
			return "the branch is pushed and the pull request opened"
		}
		return "the work is committed, merged to main, and pushed"
	case workflowPullRequests:
		return "the branch is pushed and the pull request opened"
	case workflowPullRequestsAndMerge:
		return "the branch is pushed, the pull request is opened, the checks go green, the review gate passes, and the merge commit lands"
	case workflowPatchOnly:
		return "the patch is produced and posted as a comment"
	default:
		if ref.Forge == forgeGitHub {
			return "the branch is pushed and the pull request opened"
		}
		return "the work is committed, merged to main, and pushed"
	}
}
