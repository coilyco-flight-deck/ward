package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// agent_director_prmerge.go wires the heartbeat's narrow PR-merge sweep onto the
// explicit director merge policy in agent_director_merge.go.

// directorMergeEligiblePullRequests sweeps ward-owned PRs that already satisfy the
// explicit director merge boundary and merges them through Forgejo.
func (r *Runner) directorMergeEligiblePullRequests(ctx context.Context, label string, repos []string) error {
	prClient := r.hostForgejoClient(ctx)
	issueClient, err := r.hostTrackerClient(ctx, trackerForgejo, currentAgentMode())
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for _, repo := range repos {
		owner, name, _ := strings.Cut(repo, "/")
		prs, lerr := prClient.ListOpenPullRequests(ctx, owner, name, 50)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "%s: note: cannot list pull requests in %s (%v); skipping this repo\n", label, repo, lerr)
			continue
		}
		for _, pr := range prs {
			if err := r.mergeEligibleDirectorPR(ctx, label, prClient, issueClient, owner, name, pr); err != nil {
				fmt.Fprintf(os.Stderr, "%s: note: could not finish PR %s/%s#%d: %v\n", label, owner, name, pr.Number, err)
			}
		}
	}
	return nil
}

func (r *Runner) mergeEligibleDirectorPR(ctx context.Context, label string, prClient *forgejoClient, issueClient Tracker, owner, name string, pr directorPullRequest) error {
	ok, reason, linked, meta := directorMergeEligibility(ctx, owner, name, pr, prClient, issueClient)
	if !ok {
		reportHumanInterventionBlock(ctx, owner, name, linked, pr.Number, reason, issueClient, prClient)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "%s: not merging %s/%s#%d - %s\n", label, owner, name, pr.Number, reason)
		}
		return nil
	}
	if err := prClient.mergePullRequestWithHead(ctx, owner, name, pr.Number, meta.PRHeadSHA); err != nil {
		return fmt.Errorf("merge failed for %s/%s#%d (issue #%d, workflow %s, review %q, head %s, status %s): %w",
			owner, name, pr.Number, linked, meta.Workflow, meta.Review, meta.PRHeadSHA, meta.Status.summary(), err)
	}
	if err := recordDirectorMergeDone(ctx, issueClient, prClient, owner, name, linked, pr.Number, meta); err != nil {
		return fmt.Errorf("merged %s/%s#%d for issue #%d but could not record done: %w", owner, name, pr.Number, linked, err)
	}
	fmt.Fprintf(os.Stderr, "%s: merged eligible PR %s/%s#%d for issue #%d (head %s, status %s)\n", label, owner, name, pr.Number, linked, meta.PRHeadSHA, meta.Status.summary())
	return nil
}
