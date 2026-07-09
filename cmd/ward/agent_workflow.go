package main

// agent_workflow.go carries the workflow-mode axis (ward#508): direct-main,
// pull-requests, pull-requests-and-merge, or patch-only.

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
)

// workflowMode is the landing policy for a run. The zero value reads as
// pull-requests, so plans built without --workflow follow the safe gate.
type workflowMode string

const (
	// workflowDirectMain is today's fast path: carry the issue through commit, merge
	// to main, push, and close. The trusted-repo default (ward#508).
	workflowDirectMain workflowMode = "direct-main"
	// workflowPR carries the work to a branch + pull request instead of landing on
	// main directly; a human (or a follow-up loop) is the merge gate.
	workflowPR workflowMode = "pull-requests"
	// workflowPRAndMerge carries the work to a branch + pull request, then lets the
	// director merge that PR later when policy says it is eligible.
	workflowPRAndMerge workflowMode = "pull-requests-and-merge"
	// workflowPatchOnly produces a patch/diff and reports it in a comment with NO
	// landing authority: it neither pushes main nor opens a PR (untrusted targets).
	workflowPatchOnly workflowMode = "patch-only"

	// defaultWorkflow is the mode a run takes when --workflow and smart defaults
	// leave it unset. PR is the safe product default (ward#707).
	defaultWorkflow = workflowPR
)

// orDefault collapses the "" zero value onto the default so every helper can read
// a concrete mode without each caller re-checking for empty.
func (w workflowMode) orDefault() workflowMode {
	if w == "" {
		return defaultWorkflow
	}
	return w
}

// landsOnMain reports whether this workflow may push/merge to main.
// Only direct-main does.
func (w workflowMode) landsOnMain() bool {
	return w.orDefault() == workflowDirectMain
}

// workflowChoices renders the supported --workflow values as a pipe list for flag
// usage and error text, mirroring agentHarnessChoices.
func workflowChoices() string {
	return strings.Join([]string{
		string(workflowDirectMain), string(workflowPR), string(workflowPRAndMerge), string(workflowPatchOnly),
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
	case string(workflowPR):
		return workflowPR, nil
	case string(workflowPRAndMerge):
		return workflowPRAndMerge, nil
	case string(workflowPatchOnly):
		return workflowPatchOnly, nil
	case "pr":
		return workflowPR, nil
	default:
		return "", fmt.Errorf("invalid --workflow %q: want %s", s, workflowChoices())
	}
}

// workflowFlag is the visible --workflow selector shared by the detached engineer
// surfaces (a bare ref, `engineer`, freeform). Defaults to the safe pull-requests gate.
func workflowFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "workflow",
		Value: string(defaultWorkflow),
		Usage: "landing policy for the run: " + workflowChoices() + " (default pull-requests unless smart defaults override). " +
			"direct-main merges to main; pull-requests opens a pull request; pull-requests-and-merge opens a pull request the director may later merge; patch-only reports a patch and lands nothing. `pr` remains a compatibility alias for pull-requests.",
	}
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
	case workflowDirectMain:
		return forgeCarryClause(ref)
	case workflowPR:
		return prWorkflowCarryClause(ref, false)
	case workflowPRAndMerge:
		return prWorkflowCarryClause(ref, true)
	case workflowPatchOnly:
		return patchOnlyCarryClause(ref)
	default:
		return forgeCarryClause(ref)
	}
}

// prWorkflowCarryClause tells the agent to land via a pull request, never a direct
// main push - already the GitHub default, an override of Forgejo's merge fast path.
func prWorkflowCarryClause(ref agentIssueRef, directorMerge bool) string {
	extra := ""
	if directorMerge {
		extra = fmt.Sprintf(
			" Include a `ward.workflow: %s` marker in the pull request body so the director "+
				"can later merge it once the policy boundary is satisfied.",
			workflowPRAndMerge)
	}
	if ref.Forge == forgeGitHub {
		// GitHub's forge clause is already branch + PR (main is protected), so reuse it.
		return forgeCarryClause(ref) + extra
	}
	return fmt.Sprintf(
		"implement on a feature branch, commit, push the branch to origin, and open a pull request "+
			"against `main` whose body carries `closes #%d`. Do NOT push to `main` directly or merge it "+
			"yourself - in the `pull-requests` workflow the pull request IS the merge gate, landed by a human or a "+
			"follow-up loop, not by you.%s",
		ref.Number, extra)
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
// direct-main folds in the forge; the other modes override.
func workflowLandingPhrase(ref agentIssueRef, wf workflowMode) string {
	switch wf.orDefault() {
	case workflowDirectMain:
		if ref.Forge == forgeGitHub {
			return "the branch is pushed and the pull request opened"
		}
		return "the work is committed, merged to main, and pushed"
	case workflowPR:
		return "the branch is pushed and the pull request opened"
	case workflowPRAndMerge:
		return "the branch is pushed and the pull request opened"
	case workflowPatchOnly:
		return "the patch is produced and posted as a comment"
	default:
		if ref.Forge == forgeGitHub {
			return "the branch is pushed and the pull request opened"
		}
		return "the work is committed, merged to main, and pushed"
	}
}
