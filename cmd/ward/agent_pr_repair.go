package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type prRepairBucket string

const (
	prRepairBucketCIParityGap  prRepairBucket = "ci-parity-gap"
	prRepairBucketMainRed      prRepairBucket = "main-red"
	prRepairBucketMergeQueue   prRepairBucket = "merge-queue-churn"
	prRepairBucketPRRegression prRepairBucket = "pr-regression"
)

type prRepairAssessment struct {
	Bucket       prRepairBucket
	Note         string
	WorkflowID   string
	MainState    string
	Mergeability string
}

type prRepairForgejoClassifier interface {
	getBranch(context.Context, string, string, string) (*forgejoBranch, error)
	getCommitCombinedStatus(context.Context, string, string, string) (*forgejoCommitCombinedStatus, error)
	listActionRuns(context.Context, string, string, int) ([]forgejoActionRun, error)
}

// classifyForgejoPRRepair classifies the next repair step for one Forgejo PR.
// It prefers concrete forge data over prompt heuristics for a bucketed note.
func classifyForgejoPRRepair(ctx context.Context, cl prRepairForgejoClassifier, owner, repo string, pr agentPullRequestContext) (prRepairAssessment, error) {
	assessment := prRepairAssessment{Bucket: prRepairBucketPRRegression, Mergeability: strings.TrimSpace(pr.Mergeability)}
	headSHA := strings.TrimSpace(pr.HeadSHA)
	if headSHA == "" {
		assessment.Bucket = ""
		assessment.Note = "PR head SHA is missing; keeping the current repair path"
		return assessment, nil
	}
	headStatus, err := cl.getCommitCombinedStatus(ctx, owner, repo, headSHA)
	if err != nil {
		return assessment, fmt.Errorf("classify PR repair %s/%s#%s: read head status: %w", owner, repo, headSHA, err)
	}
	if strings.ToLower(strings.TrimSpace(headStatus.State)) == "success" {
		assessment.Bucket = ""
		return assessment, nil
	}
	workflowID := latestFailedWorkflowID(headSHA, headStatus, func() ([]forgejoActionRun, error) {
		return cl.listActionRuns(ctx, owner, repo, 20)
	})
	assessment.WorkflowID = workflowID
	if workflowID == "" {
		assessment.Note = "could not identify the failing workflow; keeping the current repair path"
		return assessment, nil
	}
	if !repoHasWardExecVerb(workflowID) {
		assessment.Bucket = prRepairBucketCIParityGap
		assessment.Note = fmt.Sprintf("failing workflow %q has no local `ward exec %s` mirror in .ward/ward.yaml", workflowID, workflowID)
		return assessment, nil
	}
	branch, err := cl.getBranch(ctx, owner, repo, strings.TrimSpace(pr.BaseRef))
	if err != nil {
		return assessment, fmt.Errorf("classify PR repair %s/%s#%s: read base branch: %w", owner, repo, strings.TrimSpace(pr.BaseRef), err)
	}
	baseSHA := strings.TrimSpace(branch.Commit.ID)
	if baseSHA != "" {
		baseStatus, berr := cl.getCommitCombinedStatus(ctx, owner, repo, baseSHA)
		if berr != nil {
			return assessment, fmt.Errorf("classify PR repair %s/%s#%s: read base status: %w", owner, repo, baseSHA, berr)
		}
		if strings.ToLower(strings.TrimSpace(baseStatus.State)) != "success" {
			assessment.Bucket = prRepairBucketMainRed
			assessment.MainState = strings.TrimSpace(baseStatus.State)
			assessment.Note = fmt.Sprintf("origin/%s is %s on %s; stop blaming the PR and route to the main-failure path", emptyDefault(strings.TrimSpace(pr.BaseRef), "main"), emptyDefault(assessment.MainState, "unknown"), baseSHA)
			return assessment, nil
		}
	}
	if mergeQueueChurn(pr.Mergeability) {
		assessment.Bucket = prRepairBucketMergeQueue
		assessment.Note = fmt.Sprintf("mergeability is %s; refresh or rebase the branch once before dispatching another repair engineer", emptyDefault(strings.TrimSpace(pr.Mergeability), "unknown"))
		return assessment, nil
	}
	assessment.Bucket = prRepairBucketPRRegression
	assessment.Note = fmt.Sprintf("workflow %q is mirrored locally and origin/%s is green; keep the current engineer repair behavior", workflowID, emptyDefault(strings.TrimSpace(pr.BaseRef), "main"))
	return assessment, nil
}

func latestFailedWorkflowID(headSHA string, headStatus *forgejoCommitCombinedStatus, runs func() ([]forgejoActionRun, error)) string {
	if workflowID := latestFailedWorkflowFromRuns(headSHA, runs); workflowID != "" {
		return workflowID
	}
	return firstFailedStatusContext(headStatus)
}

func latestFailedWorkflowFromRuns(headSHA string, runs func() ([]forgejoActionRun, error)) string {
	if runs == nil {
		return ""
	}
	items, err := runs()
	if err != nil {
		return ""
	}
	headSHA = strings.TrimSpace(headSHA)
	for _, run := range items {
		if strings.TrimSpace(run.CommitSHA) != headSHA {
			continue
		}
		if strings.ToLower(strings.TrimSpace(run.Status)) == "success" {
			continue
		}
		if workflowID := strings.TrimSpace(run.WorkflowID); workflowID != "" {
			return workflowID
		}
	}
	return ""
}

func firstFailedStatusContext(headStatus *forgejoCommitCombinedStatus) string {
	if headStatus == nil {
		return ""
	}
	for _, st := range headStatus.Statuses {
		state := strings.ToLower(strings.TrimSpace(st.effectiveState()))
		if state == "" || state == "success" {
			continue
		}
		if ctx := strings.TrimSpace(st.Context); ctx != "" {
			return ctx
		}
		if ctx := strings.TrimSpace(st.Status); ctx != "" {
			return ctx
		}
	}
	return ""
}

func mergeQueueChurn(mergeability string) bool {
	mergeability = strings.ToLower(strings.TrimSpace(mergeability))
	switch {
	case mergeability == "":
		return true
	case strings.Contains(mergeability, "unknown"):
		return true
	case strings.Contains(mergeability, "false"):
		return true
	case strings.Contains(mergeability, "mergeable=false"):
		return true
	}
	return false
}

func repoHasWardExecVerb(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	cfg, err := loadDefault()
	if err != nil || cfg == nil {
		return false
	}
	for _, cmd := range cfg.Commands {
		if strings.TrimSpace(cmd.Name) == name {
			return true
		}
	}
	return false
}

func annotateForgejoPRRepair(ctx context.Context, cl *forgejoClient, owner, repo string, pr *agentPullRequestContext, ref agentIssueRef, mode containerMode) {
	if pr == nil {
		return
	}
	assessment, err := classifyForgejoPRRepair(ctx, cl, owner, repo, *pr)
	if err != nil {
		writef(os.Stderr, "%s: note: could not classify PR repair for %s: %v\n", agentCmdline(mode, "engineer"), ref, err)
		return
	}
	if assessment.Bucket == "" {
		return
	}
	pr.RepairBucket = string(assessment.Bucket)
	pr.RepairNote = assessment.Note
	writef(os.Stderr, "%s: PR repair classification: %s - %s\n", agentCmdline(mode, "engineer"), assessment.Bucket, assessment.Note)
}
