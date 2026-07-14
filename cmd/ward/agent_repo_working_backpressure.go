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
	label          string
	repo           string
	blocked        int
	running        int
	freshIntents   int
	stalePrelaunch int
	limit          int
}

func (e *engineerRepoWorkingBackpressureError) Error() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%d running", e.running))
	if e.freshIntents > 0 {
		parts = append(parts, fmt.Sprintf("%d fresh launch intent(s)", e.freshIntents))
	}
	if e.stalePrelaunch > 0 {
		parts = append(parts, fmt.Sprintf("%d stale prelaunch hold(s)", e.stalePrelaunch))
	}
	parts = append(parts, fmt.Sprintf("limit %d", e.limit))
	hint := "pass --override-capacity to exceed real running capacity"
	if e.stalePrelaunch > 0 {
		hint = "pass --override-reservation to bypass stale prelaunch holds, or --override-capacity to exceed real running capacity"
	}
	return fmt.Sprintf(
		"%s: repo engineer limit is reached for %s: %d active engineer(s) (%s); %s",
		e.label, e.repo, e.blocked, strings.Join(parts, ", "), hint,
	)
}

func newEngineerRepoWorkingBackpressureError(label, repo string, blocked, running, freshIntents, stalePrelaunch, limit int) error {
	return &engineerRepoWorkingBackpressureError{
		label:          label,
		repo:           repo,
		blocked:        blocked,
		running:        running,
		freshIntents:   freshIntents,
		stalePrelaunch: stalePrelaunch,
		limit:          limit,
	}
}

// launchRepoEngineerBackpressureCheck refuses engineer launches once the repo
// hits the carried issue/repo authority's tracker-aware active-engineer limit.
func (r *Runner) launchRepoEngineerBackpressureCheck(ctx context.Context, label string, ref agentIssueRef, overrideReservation bool) error {
	snapshot, err := r.repoEngineerBackpressureSnapshot(ctx, ref)
	if err != nil {
		return fmt.Errorf("%s: count repo engineer launches for backpressure: %w", label, err)
	}
	limit := engineerRepoWorkingLimitDefault()
	blocked := snapshot.blockedCount(overrideReservation)
	if blocked < limit {
		return nil
	}
	return newEngineerRepoWorkingBackpressureError(label, ref.repoSlug(), blocked, snapshot.Running, snapshot.freshLaunchIntents(), snapshot.StalePrelaunch, limit)
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
// It falls back to local cache only when the tracker cannot scan repository issues yet.
func (r *Runner) activeEngineerLaunchCountForRepo(ctx context.Context, ref agentIssueRef) (int, error) {
	snapshot, err := r.repoEngineerBackpressureSnapshot(ctx, ref)
	if err != nil {
		return 0, err
	}
	return snapshot.Active, nil
}

type repoEngineerBackpressureSnapshot struct {
	Active         int
	Running        int
	StalePrelaunch int
}

func (s repoEngineerBackpressureSnapshot) freshLaunchIntents() int {
	fresh := s.Active - s.Running - s.StalePrelaunch
	if fresh < 0 {
		return 0
	}
	return fresh
}

func (s repoEngineerBackpressureSnapshot) blockedCount(overrideReservation bool) int {
	blocked := s.Active
	if !overrideReservation {
		blocked += s.StalePrelaunch
	}
	if blocked < 0 {
		return 0
	}
	return blocked
}

// repoEngineerBackpressureSnapshot counts the repo's current launch pressure and
// separates visible running engineers from stale prelaunch holds.
func (r *Runner) repoEngineerBackpressureSnapshot(ctx context.Context, ref agentIssueRef) (repoEngineerBackpressureSnapshot, error) {
	repo := strings.TrimSpace(ref.repoSlug())
	if repo == "" {
		return repoEngineerBackpressureSnapshot{}, fmt.Errorf("backpressure: malformed repo %q", ref.repoSlug())
	}
	rows, err := r.agentListRows(ctx)
	if err != nil {
		return repoEngineerBackpressureSnapshot{}, err
	}
	active := 0
	running := 0
	for _, row := range rows {
		if row.Repo != repo {
			continue
		}
		active++
		if row.Phase == agentLaunchPhaseRunning {
			running++
		}
	}
	stale, err := r.stalePrelaunchReservations(ctx, time.Now().UTC(), map[string]bool{repo: true})
	if err != nil {
		return repoEngineerBackpressureSnapshot{}, err
	}
	return repoEngineerBackpressureSnapshot{
		Active:         active,
		Running:        running,
		StalePrelaunch: len(stale),
	}, nil
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
