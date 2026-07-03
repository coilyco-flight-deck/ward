package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_drain_exit.go is the ward#510 "shortly after exit" drain point: a detached
// waiter that drains a run the moment its container exits. See docs/drain-timing.md.

// drainWaiterArgv is the argv the detached waiter re-enters ward with. Pure, so a
// test pins that it routes to the hidden `container drain-exit <name>` leaf.
func drainWaiterArgv(name string) []string {
	return []string{"container", "drain-exit", name}
}

// containerDrainExitCommand wires the hidden `container drain-exit <name>` leaf: the
// detached waiter that blocks on the exit and drains the run (docs/drain-timing.md).
func containerDrainExitCommand() *cli.Command {
	return &cli.Command{
		Name:      "drain-exit",
		Hidden:    true, // spawned detached by `ward agent` launch, not by hand
		Usage:     "Wait for one ward container to exit, then drain its console/transcript/meta to the host archive.",
		ArgsUsage: "<container>",
		Description: `drain-exit is the ward#510 earliest-point drain. Launch spawns it detached for
each fire-and-forget run; it blocks on ` + "`docker wait <container>`" + ` and, the
moment the container exits, drains it to ~/.ward/agent-logs/<container>/ (or the
resolved sink). Idempotent with the later keep-10 sweep. Not invoked by hand.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "container.drain-exit",
				SkipPolicy: true, // reads docker state only; never mutates a repo tree
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runDrainExit(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runDrainExit blocks until the named container exits (or is gone), then drains it
// once. Best-effort: a missing name or a raced removal still returns cleanly.
func (r *Runner) runDrainExit(ctx context.Context, c *cli.Command) error {
	name := c.Args().First()
	if name == "" {
		return fmt.Errorf("ward container drain-exit: a container name is required")
	}
	r.drainOnExit(ctx, name)
	return nil
}

// drainOnExit blocks on the container's exit, then drains it idempotently. It is the
// body behind `container drain-exit`, factored out so the waiter path stays thin.
func (r *Runner) drainOnExit(ctx context.Context, name string) {
	fmt.Fprintf(os.Stderr, "ward container: drain-on-exit waiter waiting for %s to exit\n", name)
	// `docker wait` returns on exit OR removal; a wait error (already gone) still
	// falls through to a best-effort drain attempt.
	_ = r.Runner.Exec(ctx, "docker", "wait", name)
	r.drainAgentRunIdempotent(ctx, name, agentLogsDir())
}

// spawnDrainWaiter starts the detached `container drain-exit <name>` grandchild that
// drains the run the moment it exits - best-effort (ward#510; docs/drain-timing.md).
func (r *Runner) spawnDrainWaiter(name string) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container: could not resolve self to spawn a drain-on-exit waiter for %s (%v); the keep-10 sweep will drain it later\n", name, err)
		return
	}
	cmd := exec.Command(exe, drainWaiterArgv(name)...) // #nosec G204 -- fixed self-re-exec argv, name is our own container id
	// Force argv[0] to "ward" so the `warded`/shim multicall rewrite in main.go
	// (which keys on the basename) never re-routes this into `ward agent`.
	cmd.Args[0] = "ward"
	cmd.Env = os.Environ()
	// Detach: no controlling terminal, no inherited stdio, its own session so it
	// outlives this process and never writes to the operator's console. The session
	// detach is OS-specific (setsid on Unix, no-op on Windows); see detachProcess.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detachProcess(cmd)
	if serr := cmd.Start(); serr != nil {
		fmt.Fprintf(os.Stderr, "ward container: could not spawn a drain-on-exit waiter for %s (%v); the keep-10 sweep will drain it later\n", name, serr)
		return
	}
	// Release so this process doesn't hold the child as a zombie once it returns.
	_ = cmd.Process.Release()
	fmt.Fprintf(os.Stderr, "ward container: spawned drain-on-exit waiter for %s (pid %d)\n", name, cmd.Process.Pid)
}
