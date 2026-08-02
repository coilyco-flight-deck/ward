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
// or peer, or clears one confirmed stale launch through the dispatch broker.

// agentStopCommand builds `ward agent stop <ref>`: forward a stop request through
// the dispatch broker. A meta verb (agentMetaCommands), not a startup role.
func agentStopCommand() *cli.Command {
	return &cli.Command{
		Name:      "stop",
		Usage:     "Stop a running engineer or broker-admitted peer, or clear a confirmed stale launch.",
		ArgsUsage: "<owner/repo#N | #N | container-name | peer-id>",
		Description: `stop halts one running engineer container - the deliberate counterpart to
` + "`ward agent reap`" + `'s idle sweep (#376). Where reap stops engineers idle past a
threshold, stop targets one named engineer on demand: a director that mis-scoped an
issue and dispatched against it can halt the run instead of commenting a correction
and hoping it notices.

It runs only from a director read-only surface (the dispatch broker addr is set):
the request is forwarded to broker Ward, which resolves the target through the same
stoppability check the real stop path uses and ` + "`docker stop`" + `s it via the same
graceful path reap uses. For an issue ref whose local launch-confirmation window has
elapsed with no running container, stop clears the stale issue reservation and local
cache instead. ` + "`--print`" + ` previews either action. Off a surface it errors, like a
ref-mode dispatch does.

Issue and container-name stops remain engineer-only. A broker-minted peer id
stops only the generic peer carrying the matching ` + "`ward.peer`" + ` label.
Director, QA, session, and broker containers remain protected, and ambiguous
matches fail closed.

  ward agent stop coilyco-flight-deck/ward#625   # stop the engineer carrying #625
  ward agent stop #625                           # owner/repo inferred from the cwd origin
  ward agent stop engineer-claude-ward-625       # stop by container name
  ward agent stop critic-ab45                    # stop by broker peer id
  ward agent stop #625 --print                   # resolve + show the stoppable target, stop nothing

See docs/agent-stop.md.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "print", Usage: "resolve + show the stoppable target, or report why the ref is not stoppable, and exit"},
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

// runAgentStop resolves the target and forwards it through the supervised broker
// (ward#627). --print resolves and shows the target, running nothing.
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
		return r.forwardAgentStopToHostBroker(ctx, target, true)
	}
	return r.forwardAgentStopToHostBroker(ctx, target, false)
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
func (r *Runner) forwardAgentStopToHostBroker(ctx context.Context, target string, preview bool) error {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		if !validDispatchAgentID(target) {
			return agentStopSurfaceError()
		}
		return r.stopHostCollaborationPeer(ctx, target, preview)
	}
	if err := probeHostDispatchBroker(ctx, addr); err != nil {
		return err
	}
	req := dispatchBrokerRequest{
		Action:    dispatchActionStop,
		Target:    target,
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Preview:   preview,
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	// The stop handler returns the stopped container name in the response's log-path slot.
	name, err := sendDispatchBrokerRequest(ctx, addr, req)
	if err != nil {
		return err
	}
	// Captured as tool output by the surface agent, not written to the raw TTY.
	if ref, ok := strings.CutPrefix(name, staleLaunchCleanupResultPrefix); ok {
		if preview {
			writef(os.Stderr, "ward agent stop: would clear stale launch record %s on host ward\n", ref)
			return nil
		}
		writef(os.Stderr, "ward agent stop: cleared stale launch record %s on host ward\n", ref)
		return nil
	}
	if preview {
		writef(os.Stderr, "ward agent stop: would stop engineer container %s on host ward\n", name)
		return nil
	}
	writef(os.Stderr, "ward agent stop: stopped engineer container %s on host ward\n", name)
	return nil
}

func agentStopSurfaceError() error {
	return fmt.Errorf("ward agent stop only works from a director read-only surface with a host "+
		"dispatch broker, except for a broker-minted peer id visible on this host (%s is unset here); "+
		"see docs/agent-stop.md", envDispatchBrokerAddr)
}

func (r *Runner) stopHostCollaborationPeer(ctx context.Context, peerID string, preview bool) error {
	peers, err := r.containersForPeerID(ctx, peerID, currentDispatchClusterID(), false)
	if err != nil {
		return fmt.Errorf("ward agent stop: this host-side path accepts only a broker-minted peer id: %w", err)
	}
	name, err := selectSinglePeerTarget("stop", peerID, peers)
	if err != nil {
		if len(peers) == 0 {
			return agentStopSurfaceError()
		}
		return fmt.Errorf("ward agent stop: outside a director surface, only an unambiguous running peer id is stoppable: %w", err)
	}
	if _, err := r.guardPeerStop(ctx, name, peerID); err != nil {
		return err
	}
	if preview {
		writef(os.Stderr, "ward agent stop: would stop collaboration peer %s (container %s)\n", peerID, name)
		return nil
	}
	if err := r.dockerExec(ctx, "stop", name); err != nil {
		return fmt.Errorf("ward agent stop: docker stop %s: %w", name, err)
	}
	clusterID, err := r.containerClusterLabel(ctx, name)
	if err != nil {
		return fmt.Errorf("ward agent stop: stopped %s but could not read its cluster identity: %w", name, err)
	}
	if err := retireDispatchPeer(clusterID, peerID); err != nil {
		return fmt.Errorf("ward agent stop: stopped %s but could not retire peer %s: %w", name, peerID, err)
	}
	writef(os.Stderr, "ward agent stop: stopped collaboration peer %s (container %s)\n", peerID, name)
	return nil
}
