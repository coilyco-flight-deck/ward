package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

// agent_director_prpressure.go burns down PR backpressure before the director
// launches fresh issue work into a repo that already has too many open PRs.

const directorPRPressureMergeCue = "branch still conflicts with main"

type directorPRPressureCandidate struct {
	repo string
	pr   directorPullRequest
}

func (r *Runner) directorBurnDownOpenPRPressure(ctx context.Context, label string, repos []string) (map[string]bool, error) {
	blocked := map[string]bool{}
	if len(repos) == 0 {
		return blocked, nil
	}
	prClient := r.hostForgejoClient(ctx)
	issueClient, err := r.hostTrackerClient(ctx, trackerForgejo, currentAgentMode())
	if err != nil {
		return blocked, fmt.Errorf("resolve Forgejo tracker client: %w", err)
	}
	limit := engineerOpenPRBranchLimitDefault()
	var candidates []directorPRPressureCandidate
	for _, repo := range repos {
		repoBlocked, repoCandidates := r.directorBurnDownOpenPRPressureRepo(ctx, label, prClient, issueClient, repo, limit)
		if repoBlocked {
			blocked[repo] = true
		}
		candidates = append(candidates, repoCandidates...)
	}
	sortDirectorPRPressureCandidates(candidates)
	for _, cand := range candidates {
		owner, name, _ := strings.Cut(cand.repo, "/")
		if err := prClient.UpdatePullRequestBranch(ctx, owner, name, cand.pr.Number, ""); err != nil {
			fmt.Fprintf(os.Stderr, "%s: note: could not update %s/%s#%d automatically - %v\n", label, owner, name, cand.pr.Number, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "%s: updated PR branch %s/%s#%d to reduce open-PR pressure\n", label, owner, name, cand.pr.Number)
		break
	}
	return blocked, nil
}

func (r *Runner) directorBurnDownOpenPRPressureRepo(ctx context.Context, label string, prClient *forgejoClient, issueClient Tracker, repo string, limit int) (bool, []directorPRPressureCandidate) {
	owner, name, _ := strings.Cut(repo, "/")
	prs, err := prClient.ListOpenPullRequests(ctx, owner, name, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: cannot read PR pressure in %s (%v); continuing\n", label, repo, err)
		return false, nil
	}
	if len(prs) < limit {
		return false, nil
	}
	sortDirectorPullRequests(prs)
	var candidates []directorPRPressureCandidate
	for _, pr := range prs {
		ok, reason, _, _ := directorMergeEligibility(ctx, owner, name, pr, prClient, issueClient)
		if ok {
			continue
		}
		if !directorPRPressureCanUpdate(reason) {
			fmt.Fprintf(os.Stderr, "%s: note: not updating %s/%s#%d - %s\n", label, owner, name, pr.Number, reason)
			continue
		}
		candidates = append(candidates, directorPRPressureCandidate{repo: repo, pr: pr})
	}
	return true, candidates
}

func directorPRPressureCanUpdate(reason string) bool {
	return strings.Contains(reason, directorPRPressureMergeCue)
}

func sortDirectorPullRequests(prs []directorPullRequest) {
	sort.SliceStable(prs, func(i, j int) bool {
		if !prs[i].CreatedAt.Equal(prs[j].CreatedAt) {
			return prs[i].CreatedAt.Before(prs[j].CreatedAt)
		}
		if !prs[i].UpdatedAt.Equal(prs[j].UpdatedAt) {
			return prs[i].UpdatedAt.Before(prs[j].UpdatedAt)
		}
		return prs[i].Number < prs[j].Number
	})
}

func sortDirectorPRPressureCandidates(candidates []directorPRPressureCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].pr.CreatedAt.IsZero() != candidates[j].pr.CreatedAt.IsZero() {
			return !candidates[i].pr.CreatedAt.IsZero()
		}
		if !candidates[i].pr.CreatedAt.Equal(candidates[j].pr.CreatedAt) {
			return candidates[i].pr.CreatedAt.Before(candidates[j].pr.CreatedAt)
		}
		if !candidates[i].pr.UpdatedAt.Equal(candidates[j].pr.UpdatedAt) {
			return candidates[i].pr.UpdatedAt.Before(candidates[j].pr.UpdatedAt)
		}
		if candidates[i].repo != candidates[j].repo {
			return candidates[i].repo < candidates[j].repo
		}
		return candidates[i].pr.Number < candidates[j].pr.Number
	})
}
