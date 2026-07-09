package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// agent_director_prmerge.go wires the PR-merge sweep onto merge policy.

// directorMergeEligiblePullRequests sweeps ward-owned PRs that already satisfy the
// merge boundary and merges them through the authoritative forge.
func (r *Runner) directorMergeEligiblePullRequests(ctx context.Context, label string, repos []string) {
	for _, repo := range repos {
		owner, name, _ := strings.Cut(repo, "/")
		cl, err := r.hostForgeClient(ctx, repoAuthority(owner, name), currentAgentMode())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: note: could not build issue client for %s (%v); skipping this repo\n", label, repo, err)
			continue
		}
		prs, lerr := cl.listOpenPullRequests(ctx, owner, name, 50)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "%s: note: cannot list pull requests in %s (%v); skipping this repo\n", label, repo, lerr)
			continue
		}
		for _, pr := range prs {
			ok, reason, linked, meta := directorMergeEligibility(ctx, owner, name, pr, cl)
			if !ok {
				if reason != "" {
					fmt.Fprintf(os.Stderr, "%s: not merging %s/%s#%d - %s\n", label, owner, name, pr.Number, reason)
				}
				continue
			}
			if err := cl.mergePullRequest(ctx, owner, name, pr.Number); err != nil {
				fmt.Fprintf(os.Stderr, "%s: merge failed for %s/%s#%d (issue #%d, workflow %s, review %q): %v\n",
					label, owner, name, pr.Number, linked, meta.Workflow, meta.Review, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "%s: merged eligible PR %s/%s#%d for issue #%d\n", label, owner, name, pr.Number, linked)
		}
	}
}
