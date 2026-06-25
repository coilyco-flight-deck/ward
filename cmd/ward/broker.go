package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/credseed"
	"github.com/urfave/cli/v3"
)

// broker.go wires the hidden `ward container broker` daemon: the root credential
// broker's main + socket lifecycle (ward#329 Unit B). See docs/broker.md.

const (
	// envBrokerSocket names the socket both the daemon and the dropped agent read.
	envBrokerSocket = "WARD_BROKER_SOCK"
	// defaultBrokerSocket is the socket path when neither flag nor env is set.
	defaultBrokerSocket = "/run/ward/broker.sock"
	// brokerSocketMode is group-readable, not world (root owns, agent gid joins).
	brokerSocketMode = 0o660
	// brokerOwnerPrefix mirrors the write guardfile's `restrict owner matches coily*`.
	brokerOwnerPrefix = "coily"
	// defaultAgentGID is the dropped agent's gid (entrypoint AGENT_GID default).
	defaultAgentGID = 1000
)

// containerBrokerCommand is the Hidden `ward container broker` leaf: the daemon
// the entrypoint runs as root for a read-only explore session (ward#329).
func containerBrokerCommand() *cli.Command {
	return &cli.Command{
		Name:   "broker",
		Hidden: true, // entrypoint-internal, not a hand-run verb
		Usage:  "Entrypoint-internal root credential broker daemon: serve write-tier forgejo ops over a group-readable unix socket so the dropped agent never holds the bot token.",
		Description: `broker is the privileged side of ward's root credential broker (ward#329).
Started as root by the container entrypoint before the agent drops privilege, it
holds the forgejo bot token (from FORGEJO_TOKEN) and serves the write-tier ops -
file / edit / comment issue - by shelling ` + wardKdlWriteBin + `, authorizing each
request against the write tier (cli-guard#167). The dropped explore agent dials
the socket and asks; it never sees the credential. See docs/broker.md.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "socket",
				Usage:   "unix socket path to listen on (default $" + envBrokerSocket + ", else " + defaultBrokerSocket + ")",
				Sources: cli.EnvVars(envBrokerSocket),
				Value:   defaultBrokerSocket,
			},
			&cli.IntFlag{
				Name:    "group",
				Usage:   "gid to own the socket so the dropped agent's group can dial it",
				Sources: cli.EnvVars("WARD_AGENT_GID"),
				Value:   defaultAgentGID,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runContainerBroker(ctx, c)
		},
	}
}

// runContainerBroker resolves the root-held token, opens + permissions the
// socket, then serves until a signal cancels it.
func runContainerBroker(ctx context.Context, c *cli.Command) error {
	token := os.Getenv(credseed.EnvForgejoToken)
	if token == "" {
		return fmt.Errorf("ward container broker: %s not set; the broker has no credential to hold", credseed.EnvForgejoToken)
	}

	socket := c.String("socket")
	gid := c.Int("group")

	ln, err := newBrokerListener(socket, int(gid))
	if err != nil {
		return err
	}

	exec := &wardKdlWriteExecutor{token: token}
	srv, err := broker.NewServer(ln, exec, writeTierAuthorizer(brokerOwnerPrefix))
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("ward container broker: %w", err)
	}
	srv.SetLogf(func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "ward-broker: "+format+"\n", args...)
	})

	// A signal cancels ctx; broker.Serve then closes the listener and returns
	// context.Canceled, the clean-shutdown path.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "ward-broker: serving write-tier forgejo ops on %s (socket gid %d, owner scope %s*)\n", socket, gid, brokerOwnerPrefix)
	if serr := srv.Serve(ctx); serr != nil && !errors.Is(serr, context.Canceled) {
		return fmt.Errorf("ward container broker: %w", serr)
	}
	fmt.Fprintln(os.Stderr, "ward-broker: shut down cleanly")
	return nil
}

// newBrokerListener opens the unix socket at path and permissions it group-
// readable (root:gid 0660) so only the dropped agent's group can dial it.
func newBrokerListener(path string, gid int) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("ward container broker: create socket dir: %w", err)
	}
	// A stale socket from a prior run makes net.Listen fail with EADDRINUSE.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("ward container broker: clear stale socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("ward container broker: listen on %s: %w", path, err)
	}
	// uid -1 leaves the owner (root, the daemon) untouched; only set the group.
	if err := os.Chown(path, -1, gid); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("ward container broker: chgrp socket to gid %d: %w", gid, err)
	}
	if err := os.Chmod(path, brokerSocketMode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("ward container broker: chmod socket %#o: %w", brokerSocketMode, err)
	}
	return ln, nil
}
