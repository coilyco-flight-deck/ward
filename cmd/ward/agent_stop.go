package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_stop.go wires `ward agent stop`: a director surface halts one running engineer
// through the dispatch broker - stop-only, engineer-only (ward#627, docs/agent-stop.md).

// agentStopCommand builds `ward agent stop <ref>`: forward a stop request through
// the dispatch broker. A meta verb (agentMetaCommands), not a startup role.
func agentStopCommand() *cli.Command {
	return &cli.Command{
		Name:      "stop",
		Usage:     "Stop a running engineer through the dispatch broker - director-surface, stop-only, engineer-only (ward#627).",
		ArgsUsage: "<owner/repo#N | #N | container-name>",
		Description: `stop halts one running engineer container - the deliberate counterpart to
` + "`ward agent reap`" + `'s idle sweep (#376). Where reap stops engineers idle past a
threshold, stop targets one named engineer on demand: a director that mis-scoped an
issue and dispatched against it can halt the run instead of commenting a correction
and hoping it notices.

It runs only from a director read-only surface (the dispatch broker addr is set):
the request is forwarded to host ward, which resolves the target to a container by
its ward.role=engineer + ward.repo + ward.issue labels and ` + "`docker stop`" + `s it via the
same graceful path reap uses. Off a surface it errors, like a ref-mode dispatch does.

Stop-only, engineer-only. The host broker refuses any container that is not
ward.role=engineer (advisor / director / session are never stopped), and refuses a
ref that matches zero or more than one engineer rather than guessing.

  ward agent stop coilyco-flight-deck/ward#625   # stop the engineer carrying #625
  ward agent stop #625                           # owner/repo inferred from the cwd origin
  ward agent stop engineer-claude-ward-625       # stop by container name
  ward agent stop #625 --print                   # resolve + show the target, stop nothing

See docs/agent-stop.md.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "print", Usage: "resolve + show the stop target and exit, stopping nothing"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.stop",
				SkipPolicy: true, // forwards a broker request; no repo tree to gate
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runAgentStop(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentStop resolves the target ref/name and forwards a stop request through the
// host dispatch broker (ward#627); --print resolves + shows the target, running nothing.
func (r *Runner) runAgentStop(ctx context.Context, c *cli.Command) error {
	arg := strings.TrimSpace(c.Args().First())
	if arg == "" {
		return fmt.Errorf("ward agent stop: a target is required: owner/repo#N, a bare #N, or a container name")
	}
	target, err := r.resolveAgentStopTarget(ctx, arg)
	if err != nil {
		return fmt.Errorf("ward agent stop: %w", err)
	}
	if c.Bool("print") {
		writef(os.Stderr, "ward agent stop: would stop the engineer for %q (forwarded to host ward, which resolves it by ward.role=engineer labels)\n", target)
		return nil
	}
	return r.forwardAgentStopToHostBroker(ctx, target)
}

// resolveAgentStopTarget normalizes the broker's target: an issue ref (owner/repo#N,
// or a cwd-inferred bare #N) becomes owner/repo#N, else a verbatim name (ward#627).
func (r *Runner) resolveAgentStopTarget(ctx context.Context, arg string) (string, error) {
	if _, err := parseAgentIssueRef(arg); err != nil {
		// Not an issue ref: forward as a container name for the host to match.
		//nolint:nilerr // best-effort container-name passthrough
		return arg, nil
	}
	ref, err := r.resolveAgentIssueRef(ctx, arg)
	if err != nil {
		return "", err
	}
	return ref.String(), nil
}

// forwardAgentStopToHostBroker sends a stop through the dispatch broker's TCP + token
// path (ward#627); off a surface (no broker addr) it errors, like a dispatch does.
func (r *Runner) forwardAgentStopToHostBroker(ctx context.Context, target string) error {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return fmt.Errorf("ward agent stop only works from a director read-only surface with a host "+
			"dispatch broker (%s is unset here); halt a run host-side with `docker container stop` instead "+
			"(see docs/container-stop.md)", envDispatchBrokerAddr)
	}
	req := dispatchBrokerRequest{
		Action:    dispatchActionStop,
		Target:    target,
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	// The stop handler returns the stopped container name in the response's log-path slot.
	name, err := sendDispatchBrokerRequest(ctx, addr, req)
	if err != nil {
		return err
	}
	// Captured as tool output by the surface agent, not written to the raw TTY.
	writef(os.Stderr, "ward agent stop: stopped engineer container %s on host ward\n", name)
	return nil
}
