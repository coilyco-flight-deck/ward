package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
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
	// defaultBrokerLog is deliberately separate from the shared director TUI.
	defaultBrokerLog = "/run/ward/broker.log"
	// brokerStartTimeout bounds bootstrap's wait for a usable socket.
	brokerStartTimeout = 3 * time.Second
	// brokerSocketMode is group-readable, not world (root owns, agent gid joins).
	brokerSocketMode = 0o660
	// brokerOwnerPrefix mirrors the write guardfile's `restrict owner matches coily*`.
	brokerOwnerPrefix = "coily"
	// defaultAgentGID is the dropped agent's gid (entrypoint AGENT_GID default).
	defaultAgentGID = 1000
)

var credentialBrokerCommand = func(ctx context.Context, bin, socket, gid string) *exec.Cmd {
	return exec.CommandContext(ctx, bin, "container", "broker", "--socket", socket, "--group", gid) // #nosec G204 -- current ward binary and bootstrap-derived socket/gid
}

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
file / edit / comment issue - through ward's core Forgejo adapter, authorizing
each request against the write tier (cli-guard#167). The dropped explore agent
dials the socket and asks; it never sees the credential. See docs/broker.md.`,
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
		Action: runContainerBroker,
	}
}

// runContainerBroker resolves the root-held token, opens + permissions the
// socket, then serves until a signal cancels it.
func runContainerBroker(ctx context.Context, c *cli.Command) error {
	r := &Runner{Runner: &shell.Runner{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}}
	token := os.Getenv(credseed.EnvForgejoToken)
	if token == "" {
		return fmt.Errorf("ward container broker: %s not set; the broker has no credential to hold", credseed.EnvForgejoToken)
	}

	socket := c.String("socket")
	gid := c.Int("group")

	ln, err := newBrokerListener(socket, gid)
	if err != nil {
		return err
	}

	exec := newWardKdlWriteExecutor(token, func(refreshCtx context.Context) (string, error) {
		return r.ssmValueResolver(refreshCtx, forgejoTokenSSMPath)
	})
	srv, err := broker.NewServer(ln, exec, r.writeTierAuthorizer())
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

// startCredentialBroker ports the old root broker lifecycle and fails closed.
// See docs/broker.md for the read-only credential boundary (ward#1521).
func (r *Runner) startCredentialBroker(ctx context.Context, e bootstrapEnv) error {
	if !e.ReadOnly {
		return nil
	}
	if strings.TrimSpace(os.Getenv(credseed.EnvForgejoToken)) == "" {
		// An unauthenticated read-only surface has no credential to leak and can
		// still inspect public repos. Do not synthesize a fallback token path.
		blog("broker skipped: %s is not set; Forgejo operations stay unauthenticated", credseed.EnvForgejoToken)
		return nil
	}
	socket := envOr(envBrokerSocket, defaultBrokerSocket)
	logPath := envOr("WARD_BROKER_LOG", defaultBrokerLog)
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return fmt.Errorf("ward container broker: create socket directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("ward container broker: create log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("ward container broker: open daemon log: %w", err)
	}
	bin, err := os.Executable()
	if err != nil {
		_ = logFile.Close()
		return fmt.Errorf("ward container broker: resolve ward executable: %w", err)
	}
	cmd := credentialBrokerCommand(ctx, bin, socket, e.AgentGID)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("ward container broker: start daemon: %w", err)
	}
	// Cmd.Wait owns process cleanup; the file stays open until the daemon exits.
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	deadline := time.Now().Add(brokerStartTimeout)
	for time.Now().Before(deadline) {
		if brokerSocketReachable(socket) {
			if err := os.Setenv(envBrokerSocket, socket); err != nil {
				return fmt.Errorf("ward container broker: export socket: %w", err)
			}
			blog("broker ready: exported %s=%s; daemon log at %s", envBrokerSocket, socket, logPath)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return fmt.Errorf("ward container broker: socket did not become ready within %s; see %s", brokerStartTimeout, logPath)
}

func brokerSocketReachable(socket string) bool {
	if !isSocket(socket) {
		return false
	}
	conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
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
	if err := secureBrokerSocket(path, gid); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}
