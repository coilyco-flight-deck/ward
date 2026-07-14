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
		"%s: repo engineer limit is reached for %s: %d active engineer(s) (limit %d); pass --override-capacity to exceed real running capacity",
		e.label, e.repo, e.working, e.limit,
	)
}

func newEngineerRepoWorkingBackpressureError(label, repo string, working, limit int) error {
	return &engineerRepoWorkingBackpressureError{label: label, repo: repo, working: working, limit: limit}
}

// launchRepoEngineerBackpressureCheck refuses engineer launches once the repo
// hits the carried issue/repo authority's tracker-aware active-engineer limit.
func (r *Runner) launchRepoEngineerBackpressureCheck(ctx context.Context, label string, ref agentIssueRef, overrideReservation bool) error {
	count, err := r.activeEngineerLaunchCountForRepo(ctx, ref)
	if err != nil {
		return fmt.Errorf("%s: count repo engineer launches for backpressure: %w", label, err)
	}
	limit := engineerRepoWorkingLimitDefault()
	if count < limit {
		return nil
	}
	_ = overrideReservation
	return newEngineerRepoWorkingBackpressureError(label, ref.repoSlug(), count, limit)
}

// maybeLaunchRepoEngineerBackpressure applies the repo-working gate only when the
// launch is not a print-only preview.
func (r *Runner) maybeLaunchRepoEngineerBackpressure(ctx context.Context, label string, ref agentIssueRef, c *cli.Command) error {
	if c != nil && c.Bool("print") {
		return nil
	}
	return r.launchRepoEngineerBackpressureCheck(ctx, label, ref, overrideReservation(c))
}

type repoIssueScanner interface {
	listOpenIssueFeedByType(ctx context.Context, owner, repo string, limit int, kind string) ([]forgejoIssueRaw, error)
}

type repoIssueScanUnsupportedError struct {
	tracker tracker
}

func (e *repoIssueScanUnsupportedError) Error() string {
	return fmt.Sprintf("backpressure: repo issue scan is not supported yet for %s tracker", e.tracker)
}

func isRepoIssueScanUnsupported(err error) bool {
	var unsupported *repoIssueScanUnsupportedError
	return errors.As(err, &unsupported)
}

// activeEngineerLaunchCountForRepo counts launches with carried issue/repo authority.
// It falls back to local Docker state only when the tracker cannot scan issues yet.
func (r *Runner) activeEngineerLaunchCountForRepo(ctx context.Context, ref agentIssueRef) (int, error) {
	repo := strings.TrimSpace(ref.repoSlug())
	if repo == "" {
		return 0, fmt.Errorf("backpressure: malformed repo %q", ref.repoSlug())
	}
	if count, err := r.activeEngineerLaunchCountFromIssueThread(ctx, ref); err == nil {
		return count, nil
	} else if !isRepoIssueScanUnsupported(err) {
		return 0, err
	}
	running, err := r.runningEngineerContainersForRepo(ctx, repo)
	if err != nil {
		return 0, err
	}
	return len(running), nil
}

// runningEngineerContainersForRepo filters the live Docker engineer list down to
// one repo when the tracker cannot provide issue-thread authority yet.
func (r *Runner) runningEngineerContainersForRepo(ctx context.Context, repo string) ([]string, error) {
	names, err := r.runningEngineerContainers(ctx)
	if err != nil {
		return nil, err
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, nil
	}
	now := time.Now().UTC()
	var out []string
	for _, name := range names {
		row, err := r.runningEngineerRow(ctx, now, name)
		if err != nil {
			continue
		}
		if strings.TrimSpace(row.Repo) == repo {
			out = append(out, name)
		}
	}
	return out, nil
}

// activeEngineerLaunchCountFromIssueThread reads issue-thread reservations
// through the carried ref's tracker-aware client, never by assuming Forgejo.
func (r *Runner) activeEngineerLaunchCountFromIssueThread(ctx context.Context, ref agentIssueRef) (int, error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(ref.repoSlug()), "/")
	if !ok || owner == "" || name == "" {
		return 0, fmt.Errorf("backpressure: malformed repo %q", ref.repoSlug())
	}
	cl, err := r.hostTrackerClient(ctx, ref.trackerOrDefault(), currentAgentMode())
	if err != nil {
		return 0, err
	}
	scanner, ok := cl.(repoIssueScanner)
	if !ok {
		return 0, &repoIssueScanUnsupportedError{tracker: ref.trackerOrDefault()}
	}
	issues, err := scanner.listOpenIssueFeedByType(ctx, owner, name, directorLimitDefault(), "issues")
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
