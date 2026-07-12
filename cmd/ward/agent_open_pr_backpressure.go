package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
)

// openPRBackpressureQueryLimit bounds the launch-gate read.
const openPRBackpressureQueryLimit = 50

// openPRBackpressureError marks a launch refusal because the repo already has
// too many open PR branches for fresh work to keep inflating the queue.
type openPRBackpressureError struct {
	label   string
	openPRs int
	limit   int
}

func (e *openPRBackpressureError) Error() string {
	return fmt.Sprintf(
		"%s: open PR backpressure is engaged: %d open PR branch(es) (limit %d); "+
			"continue with --branch on an existing PR or wait for the queue to drain",
		e.label, e.openPRs, e.limit,
	)
}

func newOpenPRBackpressureError(label string, openPRs, limit int) error {
	return &openPRBackpressureError{label: label, openPRs: openPRs, limit: limit}
}

func isOpenPRBackpressureError(err error) bool {
	var backpressureErr *openPRBackpressureError
	return errors.As(err, &backpressureErr)
}

// openPRBackpressureApplies reports whether this launch is net-new work that
// should pause once the repo is already over the open-PR cap.
func openPRBackpressureApplies(c *cli.Command, w resolvedWork) bool {
	if w.Ref.MergeRequest {
		return false
	}
	if c != nil && c.IsSet("branch") {
		if strings.TrimSpace(c.String("branch")) != "" {
			return false
		}
	}
	return true
}

// dispatchBrokerLaunchHasContinuationBranch reports whether the launch argv is
// already continuing an existing PR branch rather than creating fresh work.
func dispatchBrokerLaunchHasContinuationBranch(argv []string) bool {
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--pr":
			return true
		case "--branch":
			if i+1 < len(argv) && strings.TrimSpace(argv[i+1]) != "" {
				return true
			}
		}
	}
	return false
}

// dispatchBrokerLaunchIsPrint reports whether the launch is a print-only preview
// and should skip launch-time backpressure gating.
func dispatchBrokerLaunchIsPrint(argv []string) bool {
	for _, arg := range argv {
		if arg == "--print" {
			return true
		}
	}
	return false
}

// launchOpenPRBackpressureCheck refuses net-new work when the repo already has
// too many open PR branches.
func (r *Runner) launchOpenPRBackpressureCheck(ctx context.Context, label string, repo string, allowContinuation bool) error {
	if allowContinuation {
		return nil
	}
	count, err := r.openPRBackpressureCount(ctx, repo)
	if err != nil {
		return fmt.Errorf("%s: count open PRs for backpressure: %w", label, err)
	}
	limit := engineerOpenPRBranchLimitDefault()
	if count < limit {
		return nil
	}
	return newOpenPRBackpressureError(label, count, limit)
}

// maybeLaunchOpenPRBackpressure applies the launch gate only when the launch is
// not a print-only preview.
func (r *Runner) maybeLaunchOpenPRBackpressure(ctx context.Context, label string, repo string, c *cli.Command, w resolvedWork) error {
	if c != nil && c.Bool("print") {
		return nil
	}
	return r.launchOpenPRBackpressureCheck(ctx, label, repo, openPRBackpressureApplies(c, w))
}

// dispatchBrokerOpenPRBackpressureCheck applies the launch gate for a brokered
// engineer request and returns the refusal if the repo is already over cap.
func (r *Runner) dispatchBrokerOpenPRBackpressureCheck(ctx context.Context, req dispatchBrokerRequest, label string) error {
	if dispatchAction(req.Action) != dispatchActionLaunch || req.Role != roleEngineer {
		return nil
	}
	if dispatchBrokerLaunchIsPrint(req.Argv) || dispatchBrokerLaunchHasContinuationBranch(req.Argv) {
		return nil
	}
	ref, err := parseAgentIssueRef(req.Argv[1])
	if err != nil {
		return err
	}
	return r.launchOpenPRBackpressureCheck(ctx, label, ref.repoSlug(), false)
}

func (r *Runner) openPRBackpressureCount(ctx context.Context, repo string) (int, error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(repo), "/")
	if !ok || owner == "" || name == "" {
		return 0, fmt.Errorf("backpressure: malformed repo %q", repo)
	}
	cl := r.hostForgejoClient(ctx)
	prs, err := cl.listOpenPullRequests(ctx, owner, name, openPRBackpressureQueryLimit)
	if err != nil {
		return 0, err
	}
	return len(prs), nil
}
