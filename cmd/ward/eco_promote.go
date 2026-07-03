package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// eco_promote.go is the live-side auto-promote state machine for `ward eco test`:
// snapshot -> apply -> restart -> health + canary -> healthy|rollback. See docs/.

// ecoHealth is one health sample of the live server (eco-ops#26 signals).
type ecoHealth struct {
	ServiceActive bool   // systemd unit is active/running
	JournalClean  bool   // no ModKit load exception in the journal since restart
	ServerReady   bool   // the server reached its ready marker
	Raw           string // the underlying probe output, for diagnostics
}

// healthy reports whether every signal is green.
func (h ecoHealth) healthy() bool {
	return h.ServiceActive && h.JournalClean && h.ServerReady
}

// degradedReason names the first failing signal, for logs and rollback messages.
func (h ecoHealth) degradedReason() string {
	switch {
	case !h.ServiceActive:
		return "systemd unit not active"
	case !h.ServerReady:
		return "server not ready"
	case !h.JournalClean:
		return "ModKit load exception in journal (eco-ops#7)"
	default:
		return ""
	}
}

// EcoExecutor is the side-effecting seam the promote state machine drives (ssh
// scripts in prod, a fake in tests). Every method is a no-op under dry-run.
type EcoExecutor interface {
	// Snapshot copies live Mods/+Configs/ aside and returns an id to restore it.
	Snapshot(ctx context.Context) (snapshotID string, err error)
	// ApplyMod pushes the working-copy mod/config change onto the live server.
	ApplyMod(ctx context.Context) error
	// Restart restarts the eco-server systemd unit.
	Restart(ctx context.Context) error
	// Probe takes one health sample.
	Probe(ctx context.Context) (ecoHealth, error)
	// Rollback restores the named snapshot and restarts the unit.
	Rollback(ctx context.Context, snapshotID string) error
}

// promotePlan parameterizes the canary watch window; zeros default in normalize().
type promotePlan struct {
	// CanarySamples is how many post-restart samples the watch window takes.
	CanarySamples int
	// CanaryInterval is the wait between canary samples.
	CanaryInterval time.Duration
}

func (p promotePlan) normalize() promotePlan {
	if p.CanarySamples <= 0 {
		p.CanarySamples = 3
	}
	if p.CanaryInterval <= 0 {
		p.CanaryInterval = 20 * time.Second
	}
	return p
}

// promoteOutcome is the terminal state of a promote attempt.
type promoteOutcome int

const (
	// outcomePromoted: change is live and stayed healthy through the canary window.
	outcomePromoted promoteOutcome = iota
	// outcomeRolledBack: a failure was detected and the snapshot restored, healthy.
	outcomeRolledBack
	// outcomeRollbackFailed: failure AND the rollback did not recover; needs a human.
	outcomeRollbackFailed
	// outcomeAborted: failure before any mutation (snapshot failed), server untouched.
	outcomeAborted
)

func (o promoteOutcome) String() string {
	switch o {
	case outcomePromoted:
		return "promoted"
	case outcomeRolledBack:
		return "rolled-back"
	case outcomeRollbackFailed:
		return "rollback-failed"
	case outcomeAborted:
		return "aborted"
	default:
		return "unknown"
	}
}

// promoteResult is the full record of a promote attempt.
type promoteResult struct {
	Outcome    promoteOutcome
	SnapshotID string
	// Reason explains a non-promoted outcome (empty on outcomePromoted).
	Reason string
	// Samples is every health sample taken, in order, for the audit trail.
	Samples []ecoHealth
}

// ok reports whether the live world ended safe (promoted, reverted, or untouched).
func (r promoteResult) ok() bool {
	return r.Outcome == outcomePromoted || r.Outcome == outcomeRolledBack || r.Outcome == outcomeAborted
}

// runPromote executes the state machine; logf reports progress, sleep gates the
// canary interval (injected for tests), exec supplies the side effects.
func runPromote(
	ctx context.Context,
	plan promotePlan,
	exec EcoExecutor,
	logf func(string, ...any),
	sleep func(time.Duration),
) (promoteResult, error) {
	plan = plan.normalize()
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	// 1. Snapshot first. A snapshot failure means we never touch the server.
	logf("promote: snapshotting live Mods/ + Configs/ before any change")
	snapID, err := exec.Snapshot(ctx)
	if err != nil {
		return promoteResult{
			Outcome: outcomeAborted,
			Reason:  fmt.Sprintf("snapshot failed, live server untouched: %v", err),
		}, nil
	}
	res := promoteResult{SnapshotID: snapID}
	logf("promote: snapshot %s taken", snapID)

	// 2. Apply the mod. Any failure from here on rolls back.
	logf("promote: applying working-copy mod to live server")
	if err := exec.ApplyMod(ctx); err != nil {
		return rollback(ctx, exec, res, logf, fmt.Sprintf("apply failed: %v", err))
	}

	// 3. Restart.
	logf("promote: restarting eco-server.service")
	if err := exec.Restart(ctx); err != nil {
		return rollback(ctx, exec, res, logf, fmt.Sprintf("restart failed: %v", err))
	}

	// 4. Immediate post-restart probe.
	logf("promote: probing health after restart")
	sample, err := exec.Probe(ctx)
	if err != nil {
		return rollback(ctx, exec, res, logf, fmt.Sprintf("initial probe errored: %v", err))
	}
	res.Samples = append(res.Samples, sample)
	if !sample.healthy() {
		return rollback(ctx, exec, res, logf, "initial health probe degraded: "+sample.degradedReason())
	}
	logf("promote: initial probe healthy")

	// 5. Canary watch window: roll back on the FIRST degradation, not the whole window.
	for i := 1; i <= plan.CanarySamples; i++ {
		sleep(plan.CanaryInterval)
		logf("promote: canary sample %d/%d", i, plan.CanarySamples)
		sample, err := exec.Probe(ctx)
		if err != nil {
			return rollback(ctx, exec, res, logf, fmt.Sprintf("canary probe %d errored: %v", i, err))
		}
		res.Samples = append(res.Samples, sample)
		if !sample.healthy() {
			return rollback(ctx, exec, res, logf,
				fmt.Sprintf("canary sample %d/%d degraded: %s", i, plan.CanarySamples, sample.degradedReason()))
		}
	}

	res.Outcome = outcomePromoted
	logf("promote: healthy through %d canary samples - PROMOTED", plan.CanarySamples)
	return res, nil
}

// rollback restores the snapshot, restarts, and verifies recovery: outcomeRolledBack
// on a clean revert, outcomeRollbackFailed (loud) when the restore does not recover.
func rollback(
	ctx context.Context,
	exec EcoExecutor,
	res promoteResult,
	logf func(string, ...any),
	reason string,
) (promoteResult, error) {
	res.Reason = reason
	logf("promote: ROLLING BACK - %s", reason)

	if err := exec.Rollback(ctx, res.SnapshotID); err != nil {
		res.Outcome = outcomeRollbackFailed
		res.Reason = fmt.Sprintf("%s; AND rollback restore failed: %v", reason, err)
		logf("promote: ROLLBACK RESTORE FAILED - manual intervention required: %v", err)
		return res, nil
	}

	// A rollback that restarts but does not reach ready is not a clean revert.
	sample, err := exec.Probe(ctx)
	if err != nil {
		res.Outcome = outcomeRollbackFailed
		res.Reason = fmt.Sprintf("%s; rolled back but post-rollback probe errored: %v", reason, err)
		logf("promote: post-rollback probe errored - manual intervention required: %v", err)
		return res, nil
	}
	res.Samples = append(res.Samples, sample)
	if !sample.healthy() {
		res.Outcome = outcomeRollbackFailed
		res.Reason = fmt.Sprintf("%s; rolled back but server still unhealthy: %s", reason, sample.degradedReason())
		logf("promote: server unhealthy after rollback - manual intervention required: %s", sample.degradedReason())
		return res, nil
	}

	res.Outcome = outcomeRolledBack
	logf("promote: rolled back cleanly, live world reverted and healthy")
	return res, nil
}

// errNoSnapshot guards the executor from a rollback with no snapshot id.
var errNoSnapshot = errors.New("rollback requested with empty snapshot id")

// summaryLine renders a one-line promote verdict for terminals and issue comments.
func (r promoteResult) summaryLine() string {
	switch r.Outcome {
	case outcomePromoted:
		return fmt.Sprintf("promote: PROMOTED (healthy through %d samples)", len(r.Samples))
	case outcomeAborted:
		return "promote: ABORTED (live server untouched) - " + r.Reason
	case outcomeRolledBack:
		return "promote: ROLLED BACK (live world reverted, healthy) - " + r.Reason
	case outcomeRollbackFailed:
		return "promote: ROLLBACK FAILED - MANUAL INTERVENTION REQUIRED - " + r.Reason
	default:
		return "promote: unknown outcome"
	}
}

// dryRunPlanLines renders the ordered steps a real promote would run (no side effects).
func dryRunPlanLines(plan promotePlan) []string {
	plan = plan.normalize()
	return []string{
		"1. snapshot live Mods/ + Configs/ (eco-server-snapshot.sh)",
		"2. apply working-copy mod to live server (install-eco-mod.sh / eco mod push)",
		"3. restart eco-server.service",
		"4. probe health once (eco-server-health-check.sh: service active, journal clean, ready)",
		fmt.Sprintf("5. canary watch: %d samples every %s, roll back on first degradation",
			plan.CanarySamples, plan.CanaryInterval),
		"6. healthy -> done; any failure -> restore snapshot + restart (eco-server-rollback.sh)",
	}
}
