package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// engineerRepoWorkingBackpressureError marks a launch refusal because the repo
// already has too many active engineers.
type engineerRepoWorkingBackpressureError struct {
	label   string
	repo    string
	working int
	limit   int
}

func (e *engineerRepoWorkingBackpressureError) Error() string {
	return fmt.Sprintf(
		"%s: repo engineer limit is reached for %s: %d active engineer(s) (limit %d); wait for a run to finish before launching another engineer",
		e.label, e.repo, e.working, e.limit,
	)
}

func newEngineerRepoWorkingBackpressureError(label, repo string, working, limit int) error {
	return &engineerRepoWorkingBackpressureError{label: label, repo: repo, working: working, limit: limit}
}

// launchRepoEngineerBackpressureCheck refuses engineer launches once the repo
// already has too many active engineers working in it.
func (r *Runner) launchRepoEngineerBackpressureCheck(ctx context.Context, label string, repo string) error {
	count, err := r.activeEngineerLaunchCountForRepo(ctx, repo)
	if err != nil {
		return fmt.Errorf("%s: count repo engineer launches for backpressure: %w", label, err)
	}
	limit := engineerRepoWorkingLimitDefault()
	if count < limit {
		return nil
	}
	return newEngineerRepoWorkingBackpressureError(label, repo, count, limit)
}

// maybeLaunchRepoEngineerBackpressure applies the repo-working gate only when the
// launch is not a print-only preview.
func (r *Runner) maybeLaunchRepoEngineerBackpressure(ctx context.Context, label string, repo string, c *cli.Command) error {
	if c != nil && c.Bool("print") {
		return nil
	}
	return r.launchRepoEngineerBackpressureCheck(ctx, label, repo)
}

// activeEngineerLaunchCountForRepo counts active launches in one repo.
// It prefers the issue thread and falls back to local cache only when reads fail.
func (r *Runner) activeEngineerLaunchCountForRepo(ctx context.Context, repo string) (int, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return 0, fmt.Errorf("backpressure: malformed repo %q", repo)
	}
	if count, err := r.activeEngineerLaunchCountFromIssueThread(ctx, repo); err == nil {
		return count, nil
	}
	running, err := r.runningEngineerContainersForRepo(ctx, repo)
	if err != nil {
		return 0, err
	}
	return len(running), nil
}

func (r *Runner) activeEngineerLaunchCountFromIssueThread(ctx context.Context, repo string) (int, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return 0, fmt.Errorf("backpressure: malformed repo %q", repo)
	}
	cl := r.hostForgejoClient(ctx)
	issues, err := cl.listOpenIssueFeedByType(ctx, owner, name, directorLimitDefault(), "issues")
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	active := 0
	for _, issue := range issues {
		comments, cerr := cl.listIssueComments(ctx, owner, name, issue.Number)
		if cerr != nil {
			return 0, cerr
		}
		if _, held := freshReservationComment(comments, now, agentReservationTTL()); held {
			active++
		}
	}
	return active, nil
}
