package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
)

// agent_reap.go wires `ward agent reap`, the host-side idle-killer for wedged
// engineer containers (issue #376). The pure verdict lives in agent_reap_compute.go.

// agentReapCommand builds `ward agent reap`: sweep running engineers, stop those
// idle past the threshold. A meta verb (agentMetaCommands), not a startup role.
func agentReapCommand() *cli.Command {
	return &cli.Command{
		Name:  "reap",
		Usage: "Stop engineer containers idle past the threshold (default 1h) - the host-side idle-killer (#376).",
		Description: `reap is the host-side idle-killer for wedged engineer containers (issue #376).
A ` + "`ward agent engineer`" + ` run is fire-and-forget and exits cleanly when its work
is done, so an engineer still Up but silent for a long stretch is not working
quietly - it is wedged, holding a container slot. reap sweeps the running
engineers (` + "`ward.role=engineer`" + `), computes each one's idle time from its last
container-log line (falling back to its start time), and ` + "`docker stop`" + `s any past
the --idle threshold.

A CPU guard spares an idle container reading above --max-cpu, so a legit long
quiet build/test is not killed mid-work (an engineer commits and pushes, unlike
an architect, so a false kill has a real cost). The guard only ever spares: an
unreadable CPU still reaps on idle alone.

Only ward.role=engineer is targeted. Interactive roles (director / advisor /
session) are idle by design - sitting at a prompt is normal, not wedged - and are
left untouched.

Authored here; the fleet rollout (a launchd timer or a converged daemon) is an
ansible role in infrastructure, per the authoring-vs-rollout split. The old
setup/doctor scaffold is historical, while the live ` + "`ward setup`" + ` command now
handles the config pre-bake and diagnostics path.

  ward agent reap                 # sweep once, stop engineers idle > 1h
  ward agent reap --dry-run       # report what would be stopped, stopping nothing
  ward agent reap --idle 30m      # tighter threshold
  ward agent reap --interval 5m   # run as a standing daemon, sweeping every 5m

See docs/agent-reap.md.`,
		Flags: []cli.Flag{
			&cli.DurationFlag{Name: "idle", Value: agentReapIdleDefault(), Usage: "stop an engineer idle at least this long (default 1h)"},
			&cli.FloatFlag{Name: "max-cpu", Value: agentReapMaxCPUDefault(), Usage: "spare an idle engineer reading above this %CPU as a live build/test; pass a huge value to reap on idle alone"},
			&cli.DurationFlag{Name: "interval", Usage: "run as a standing daemon, sweeping every interval (default 0: sweep once and exit)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "report what would be stopped, stopping nothing"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.reap",
				SkipPolicy: true, // sweeps docker state + stops containers; no repo tree to gate
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runAgentReap(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentReap runs the sweep once, or loops when --interval > 0 (standing daemon);
// a looping sweep's error is logged and continues, a one-shot's is returned.
func (r *Runner) runAgentReap(ctx context.Context, c *cli.Command) error {
	threshold := c.Duration("idle")
	maxCPU := c.Float("max-cpu")
	dryRun := c.Bool("dry-run")
	interval := c.Duration("interval")
	readOnly := os.Getenv("WARD_READONLY") == "1"

	w := c.Root().Writer
	if w == nil {
		w = os.Stdout
	}

	if interval <= 0 {
		return r.agentReapSweep(ctx, threshold, maxCPU, dryRun, readOnly, w)
	}

	writef(w, "ward agent reap: standing daemon, sweeping every %s (idle >= %s)\n", interval, threshold)
	for {
		if err := r.agentReapSweep(ctx, threshold, maxCPU, dryRun, readOnly, w); err != nil {
			if readOnlyDockerUnavailableErr(err) {
				return err
			}
			writef(w, "ward agent reap: sweep error (continuing): %v\n", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// agentReapSweep is one full pass: list running engineers, stop those the verdict
// marks. Best-effort per container - a failed probe or stop is logged and skipped.
func (r *Runner) agentReapSweep(ctx context.Context, threshold time.Duration, maxCPU float64, dryRun, readOnly bool, w io.Writer) error {
	names, err := r.runningEngineerContainers(ctx)
	if err != nil {
		if readOnly && dockerUnavailableErr(err) {
			return fmt.Errorf("ward agent reap: reaping is unsupported on this read-only director surface because the Docker socket is unavailable: %w", err)
		}
		return fmt.Errorf("list running engineer containers: %w", err)
	}
	now := time.Now()
	stopped, spared := r.reapRunningEngineers(ctx, names, threshold, maxCPU, dryRun, w, now)
	cleared, err := r.reapStalePrelaunchReservations(ctx, now, nil, dryRun, w)
	if err != nil {
		writef(w, "ward agent reap: warning: could not scan stale prelaunch reservations (%v)\n", err)
	}

	if len(names) == 0 && cleared == 0 {
		writeln(w, "ward agent reap: no running engineer containers.")
		return nil
	}

	action := "stopped"
	if dryRun {
		action = "would stop"
	}
	summary := fmt.Sprintf("ward agent reap: swept %d engineer(s): %s %d, kept %d", len(names), action, stopped, spared)
	if cleared > 0 {
		summary += fmt.Sprintf(", cleared %d stale prelaunch reservation(s)", cleared)
	}
	writef(w, "%s.\n", summary)
	return nil
}

func (r *Runner) reapRunningEngineers(ctx context.Context, names []string, threshold time.Duration, maxCPU float64, dryRun bool, w io.Writer, now time.Time) (stopped, spared int) {
	for _, name := range names {
		st := r.engineerReapState(ctx, name, now)
		stop, reason := reapVerdict(st, threshold, maxCPU)
		if !stop {
			writef(w, "ward agent reap: keep %s - %s\n", name, reason)
			spared++
			continue
		}
		if dryRun {
			writef(w, "ward agent reap: WOULD stop %s - %s\n", name, reason)
			stopped++
			continue
		}
		writef(w, "ward agent reap: stopping %s - %s\n", name, reason)
		if serr := r.dockerExec(ctx, "stop", name); serr != nil {
			writef(w, "ward agent reap: stop %s failed (%v); continuing\n", name, serr)
			continue
		}
		stopped++
	}
	return stopped, spared
}

func (r *Runner) reapStalePrelaunchReservations(ctx context.Context, now time.Time, scope map[string]bool, dryRun bool, w io.Writer) (int, error) {
	stale, err := r.stalePrelaunchReservations(ctx, now, scope)
	if err != nil {
		return 0, err
	}
	cleared := 0
	for _, hold := range stale {
		ref := hold.Ref()
		if dryRun {
			writef(w, "ward agent reap: WOULD clear stale prelaunch reservation %s - %s\n", ref, hold.Reason(now))
			cleared++
			continue
		}
		writef(w, "ward agent reap: clearing stale prelaunch reservation %s - %s\n", ref, hold.Reason(now))
		if cl, cerr := r.hostTrackerClient(ctx, ref.trackerOrDefault(), hold.Mode()); cerr != nil {
			writef(w, "ward agent reap: could not build issue client to clear %s (%v); continuing\n", ref, cerr)
		} else {
			clearStalePrelaunchReservation(ctx, cl, "ward agent reap", hold)
		}
		cleared++
	}
	return cleared, nil
}

func dockerUnavailableErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"cannot connect to the docker daemon",
		"permission denied",
		"no such file or directory",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func readOnlyDockerUnavailableErr(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "reaping is unsupported on this read-only director surface because the docker socket is unavailable")
}

type stalePrelaunchReservation struct {
	Path        string
	Reservation agentReservation
}

func (s stalePrelaunchReservation) Ref() agentIssueRef {
	return agentIssueRef{Owner: s.Reservation.Owner, Repo: s.Reservation.Repo, Number: s.Reservation.Number}
}

func (s stalePrelaunchReservation) Mode() containerMode {
	mode := strings.TrimSpace(s.Reservation.Mode)
	if mode == "" {
		return currentAgentMode()
	}
	return containerMode(mode)
}

func (s stalePrelaunchReservation) Container() string {
	container := strings.TrimSpace(s.Reservation.Container)
	if container == "" {
		return issueScopedContainerName(roleEngineer, s.Mode(), targetRepo{Owner: s.Reservation.Owner, Name: s.Reservation.Repo}, s.Reservation.Number)
	}
	return container
}

func (s stalePrelaunchReservation) Reason(now time.Time) string {
	return fmt.Sprintf("launch-confirmation TTL %s elapsed (reserved %s ago)", conciseDuration(agentLaunchConfirmationTTL()), formatDuration(now.Sub(s.Reservation.At)))
}

func clearStalePrelaunchReservation(ctx context.Context, cl Tracker, label string, hold stalePrelaunchReservation) {
	(&Runner{}).releaseRemoteReservation(ctx, cl, label, hold.Mode(), hold.Ref(), hold.Container())
	if released, err := removeAgentReservationIfOwned(hold.Path, hold.Reservation); err != nil {
		fmt.Fprintf(os.Stderr, "%s: warning: could not remove stale reservation on %s (%v)\n", label, hold.Ref(), err)
	} else if !released {
		fmt.Fprintf(os.Stderr, "%s: warning: stale reservation on %s was already replaced before reap\n", label, hold.Ref())
	}
}

func (r *Runner) stalePrelaunchReservations(ctx context.Context, now time.Time, scope map[string]bool) ([]stalePrelaunchReservation, error) {
	globalDir, err := config.GlobalDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(globalDir, agentReservationsSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list stale prelaunch reservations: %w", err)
	}
	out := make([]stalePrelaunchReservation, 0, len(entries))
	for _, entry := range entries {
		hold, ok := stalePrelaunchReservationFromEntry(ctx, r, dir, entry, now, scope)
		if !ok {
			continue
		}
		out = append(out, hold)
	}
	return out, nil
}

func stalePrelaunchReservationFromEntry(ctx context.Context, r *Runner, dir string, entry os.DirEntry, now time.Time, scope map[string]bool) (stalePrelaunchReservation, bool) {
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
		return stalePrelaunchReservation{}, false
	}
	return stalePrelaunchReservationFromPath(ctx, r, filepath.Join(dir, entry.Name()), now, scope)
}

func stalePrelaunchReservationFromPath(ctx context.Context, r *Runner, path string, now time.Time, scope map[string]bool) (stalePrelaunchReservation, bool) {
	res, ok, err := readAgentReservation(path)
	if err != nil || !ok || res == nil {
		return stalePrelaunchReservation{}, false
	}
	ref := agentIssueRef{Owner: res.Owner, Repo: res.Repo, Number: res.Number}
	if ref.Owner == "" || ref.Repo == "" || ref.Number <= 0 {
		return stalePrelaunchReservation{}, false
	}
	if len(scope) > 0 && !scope[ref.repoSlug()] {
		return stalePrelaunchReservation{}, false
	}
	if reservationFresh(res.At, now, agentLaunchConfirmationTTL()) {
		return stalePrelaunchReservation{}, false
	}
	if r.containerRunning(ctx, res.Container) {
		return stalePrelaunchReservation{}, false
	}
	return stalePrelaunchReservation{Path: path, Reservation: *res}, true
}

// runningEngineerContainers lists the running engineer containers by their
// ward=true + ward.role=engineer labels (no name-regex; #376).
func (r *Runner) runningEngineerContainers(ctx context.Context) ([]string, error) {
	out, err := r.dockerCapture(ctx, "ps", "--format", "{{.Names}}",
		"--filter", "label="+containerLabel,
		"--filter", "label="+labelRole+"="+roleEngineer)
	if err != nil {
		return nil, err
	}
	// parseExitedContainerNames is a plain non-blank-line splitter (name is historical).
	return parseExitedContainerNames(string(out)), nil
}

// runningEngineerContainersForRepo lists the running engineer containers for one repo.
func (r *Runner) runningEngineerContainersForRepo(ctx context.Context, repo string) ([]string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, fmt.Errorf("list running engineer containers: malformed repo %q", repo)
	}
	out, err := r.dockerCapture(ctx, "ps", "--format", "{{.Names}}",
		"--filter", "label="+containerLabel,
		"--filter", "label="+labelRole+"="+roleEngineer,
		"--filter", "label="+labelRepo+"="+repo)
	if err != nil {
		return nil, err
	}
	// parseExitedContainerNames is a plain non-blank-line splitter (name is historical).
	return parseExitedContainerNames(string(out)), nil
}

// enforceEngineerContainerLimit refuses a new engineer launch at the OOM ceiling;
// --override-capacity grants one loud launch past it and never stacks (ward#1045).
func (r *Runner) enforceEngineerContainerLimit(ctx context.Context, label string, overrideCapacity bool) error {
	names, err := r.runningEngineerContainers(ctx)
	if err != nil {
		return fmt.Errorf("%s: count running engineer containers: %w", label, err)
	}
	limit := engineerContainerLimitDefault()
	count := len(names)
	if count < limit {
		return nil
	}
	if overrideCapacity {
		if count == limit {
			fmt.Fprintf(os.Stderr, "%s: WARNING: launching over the engineer OOM ceiling (%d/%d) - host may thrash or OOM (--override-capacity)\n", label, count+1, limit)
			return nil
		}
		fmt.Fprintf(os.Stderr, "%s: note: --override-capacity grants exactly one launch past the ceiling and the pool is already past it (%d/%d); refusing (ward#1045)\n", label, count, limit)
	}
	return newEngineerCapacityError(label, count, limit)
}

// engineerCapacityError marks a launch refusal because the global engineer cap
// is already full. It is backpressure, not a terminal launch failure.
type engineerCapacityError struct {
	label   string
	running int
	limit   int
}

func (e *engineerCapacityError) Error() string {
	return fmt.Sprintf(
		"%s: global engineer limit is reached: %d running (limit %d); wait for a run to finish, run `ward agent reap` for stale engineers, or follow docs/agent-ops.md for manual stale reservation cleanup",
		e.label, e.running, e.limit,
	)
}

func newEngineerCapacityError(label string, running, limit int) error {
	return &engineerCapacityError{label: label, running: running, limit: limit}
}

func isEngineerCapacityError(err error) bool {
	var capErr *engineerCapacityError
	return errors.As(err, &capErr)
}

// engineerReapState gathers one engineer's idle inputs: idle from its last log
// timestamp (else its start time, catching a never-logged PID1), plus CPU. Best-effort.
func (r *Runner) engineerReapState(ctx context.Context, name string, now time.Time) engineerReapState {
	st := engineerReapState{Name: name}
	if t, ok := lastDockerLogTime(r.dockerLogsTailCombined(ctx, name, 1)); ok {
		st.Idle, st.HasIdle = idleSince(now, t), true
	} else if t, ok := r.containerStartedAt(ctx, name); ok {
		st.Idle, st.HasIdle = idleSince(now, t), true
	}
	if cpu, ok := r.containerCPUPercent(ctx, name); ok {
		st.CPU, st.HasCPU = cpu, true
	}
	return st
}

// dockerLogsTailCombined captures `docker logs --timestamps --tail N` with
// stdout+stderr merged, so the last line is read whichever stream it went to.
func (r *Runner) dockerLogsTailCombined(ctx context.Context, name string, tail int) string {
	var buf bytes.Buffer
	prevOut, prevErr := r.Runner.Stdout, r.Runner.Stderr
	r.Runner.Stdout, r.Runner.Stderr = &buf, &buf
	_ = r.dockerExec(ctx, "logs", "--timestamps", "--tail", strconv.Itoa(tail), name)
	r.Runner.Stdout, r.Runner.Stderr = prevOut, prevErr
	return buf.String()
}

// containerStartedAt reads a container's start time via `docker inspect`; ok is
// false on any error or an unparseable/not-yet-started value.
func (r *Runner) containerStartedAt(ctx context.Context, name string) (time.Time, bool) {
	out, err := r.dockerCapture(ctx, "inspect", "-f", "{{.State.StartedAt}}", name)
	if err != nil {
		return time.Time{}, false
	}
	return parseDockerInspectTime(string(out))
}

// containerCPUPercent reads a container's instantaneous %CPU via a one-shot
// `docker stats`; ok is false when the reading is missing or unparseable.
func (r *Runner) containerCPUPercent(ctx context.Context, name string) (float64, bool) {
	out, err := r.dockerCapture(ctx, "stats", "--no-stream", "--format", "{{.CPUPerc}}", name)
	if err != nil {
		return 0, false
	}
	return parseCPUPercent(string(out))
}
