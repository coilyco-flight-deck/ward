package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
	"github.com/urfave/cli/v3"
)

// agent_director_merge.go wires `ward agent director merge`: the explicit PR-merge
// lane the director uses for ward-owned PRs that are authorized to land.

var directorClosingRefRE = regexp.MustCompile(`(?i)\b(?:closes|fixes|resolves)\s+#(\d+)\b`)

// directorMergeFlags keeps the merge subcommand narrow: scope + preview only.
func directorMergeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: director.default-scope from ~/.ward/config.yaml, else the cwd git origin)"},
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
		ArgsUsage:   "(scope via --repo; default: the cwd git origin)",
		Description: `merge scans open pull requests in scope and merges only the ones the ward issue thread marks as director-merge authorized: the linked issue ended with WARD-OUTCOME: done, the final comment says workflow: pull-request-and-merge, the review summary is passed, and the PR is not salvage/draft noise. pull-request still needs a human. See docs/agent-director.md and docs/agent-workflow.md.`,
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
	cl, err := r.hostForgejoClient(ctx)
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
		owner, name, _ := strings.Cut(repo, "/")
		prs, perr := cl.listOpenPullRequests(ctx, owner, name, limit)
		if perr != nil {
			return fmt.Errorf("%s: %w", label, perr)
		}
		for _, pr := range prs {
			ok, reason, linked, meta := directorMergeEligibility(ctx, owner, name, pr, cl)
			if !ok {
				skipped++
				_, _ = fmt.Fprintf(r.Runner.Stderr, "%s: skipping %s/%s#%d: %s\n", label, owner, name, pr.Number, reason)
				continue
			}
			if preview {
				_, _ = fmt.Fprintf(r.Runner.Stderr, "%s: would merge %s/%s#%d (issue #%d, workflow %s, review %q)\n",
					label, owner, name, pr.Number, linked, meta.Workflow, meta.Review)
				continue
			}
			if err := cl.mergePullRequest(ctx, owner, name, pr.Number); err != nil {
				return fmt.Errorf("%s: merge %s/%s#%d: %w", label, owner, name, pr.Number, err)
			}
			merged++
			_, _ = fmt.Fprintf(r.Runner.Stderr, "%s: merged %s/%s#%d (issue #%d)\n", label, owner, name, pr.Number, linked)
		}
	}
	_, _ = fmt.Fprintf(r.Runner.Stdout, "%s: merged %d PR(s), skipped %d\n", label, merged, skipped)
	return nil
}

// directorMergeEligibility returns whether pr is the narrow, ward-owned lane.
// The policy closes over the issue thread, not just the PR title.
func directorMergeEligibility(ctx context.Context, owner, repo string, pr dispatch.Issue, cl *forgejoClient) (ok bool, reason string, linked int, meta directorRunMeta) {
	linked, ok = directorLinkedIssueNumber(pr.Body)
	if !ok {
		return false, "no same-repo closing reference in the PR body", 0, directorRunMeta{}
	}
	if _, err := cl.getIssue(ctx, owner, repo, linked); err != nil {
		return false, "could not read linked issue: " + firstLine(err.Error()), linked, directorRunMeta{}
	}
	comments, err := cl.listIssueComments(ctx, owner, repo, linked)
	if err != nil {
		return false, "could not read linked issue comments: " + firstLine(err.Error()), linked, directorRunMeta{}
	}
	if latest, ok := latestBacklogOutcomeComment(comments); ok {
		meta = parseDirectorRunMeta(latest.Body)
		meta.CommentedBy = latest.User.Login
		meta.CommentedAt = latest.CreatedAt
	} else {
		return false, "linked issue never reached a WARD-OUTCOME comment", linked, directorRunMeta{}
	}
	return directorMergeDecision(pr, linked, meta)
}

// directorMergeDecision is the pure policy boundary for the director merge lane.
func directorMergeDecision(pr dispatch.Issue, linked int, meta directorRunMeta) (ok bool, reason string, _ int, _ directorRunMeta) {
	title := strings.ToLower(strings.TrimSpace(pr.Title))
	switch {
	case strings.HasPrefix(title, "ward salvage:"):
		return false, "salvage PRs are cleanup noise, not merge-authorized work", linked, meta
	case strings.HasPrefix(title, "wip:") || strings.HasPrefix(title, "[wip]"):
		return false, "draft PRs are not merge-authorized", linked, meta
	}
	if !meta.HasOutcome || strings.ToLower(strings.TrimSpace(meta.Outcome.Status)) != "done" {
		return false, "linked issue did not finish with WARD-OUTCOME: done", linked, meta
	}
	if strings.TrimSpace(meta.Workflow) != string(workflowPullRequestAndMerge) {
		if strings.TrimSpace(meta.Workflow) == "" {
			return false, "linked issue comment did not record the merge workflow", linked, meta
		}
		return false, "workflow " + meta.Workflow + " still needs human merge approval", linked, meta
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(meta.Review)), "passed:") {
		return false, "review gate did not pass", linked, meta
	}
	return true, "", linked, meta
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
