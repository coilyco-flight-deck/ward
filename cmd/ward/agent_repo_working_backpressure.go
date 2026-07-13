package main

import (
	"context"
	"errors"
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

func isEngineerRepoWorkingBackpressureError(err error) bool {
	var backpressureErr *engineerRepoWorkingBackpressureError
	return errors.As(err, &backpressureErr)
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

// activeEngineerLaunchCountForRepo counts fresh engineer work in one repo,
// including reservation-backed launches that have not yet shown up as containers.
func (r *Runner) activeEngineerLaunchCountForRepo(ctx context.Context, repo string) (int, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return 0, fmt.Errorf("backpressure: malformed repo %q", repo)
	}
	running, err := r.runningEngineerContainersForRepo(ctx, repo)
	if err != nil {
		return 0, err
	}
	count := len(running)
	seen := make(map[string]bool, len(running))
	for _, name := range running {
		seen[strings.TrimSpace(name)] = true
	}
	now := time.Now().UTC()
	pending, err := r.reservedEngineerRows(ctx, now, seen)
	if err != nil {
		return 0, err
	}
	for _, row := range pending {
		if row.Repo == repo {
			count++
		}
	}
	return count, nil
}
