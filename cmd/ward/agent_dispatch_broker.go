package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
)

// agent_dispatch_broker.go wires ward#378: director surfaces ask host ward to
// launch sibling engineer/advisor runs. TCP transport + token gate: ward#391.

const (
	// envDispatchBrokerAddr carries host.docker.internal:<port> the surface dials;
	// envDispatchBrokerToken the per-launch nonce it echoes back (ward#391).
	envDispatchBrokerAddr  = "WARD_DISPATCH_BROKER_ADDR"
	envDispatchBrokerToken = "WARD_DISPATCH_BROKER_TOKEN"

	// dispatchBrokerListenAddr binds an ephemeral TCP port on all interfaces so the
	// container reaches it via the docker gateway; the token is the access control.
	dispatchBrokerListenAddr = "0.0.0.0:0"
)

var errDispatchBrokerUnavailable = errors.New("dispatch broker unavailable")

type dispatchBrokerRequest struct {
	Role      string   `json:"role"`
	Argv      []string `json:"argv"`
	Requester string   `json:"requester,omitempty"`
	// Token is the per-launch shared secret the surface echoes back so the host
	// broker authenticates the dial (the TCP port has no socket file perms).
	Token string `json:"token,omitempty"`
}

type dispatchBrokerResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// LogPath is the host path the served run's stdout/stderr were redirected to,
	// so the requesting surface can name it without any bytes hitting the TTY (ward#389).
	LogPath string `json:"log_path,omitempty"`
}

// dispatchStdioMu serializes the process-global os.Stdout/os.Stderr swap that keeps
// a served run's deploy output off the shared read-only TUI (ward#389).
var dispatchStdioMu sync.Mutex

// dispatchLogsSubdir is the per-host dir under ~/.ward/agent-logs (agentLogsDir)
// holding one file per forwarded run, sibling to the drained-container archives.
const dispatchLogsSubdir = "dispatch"

// startHostDispatchBroker serves validated dispatch requests until ctx ends. It
// returns the host:port the container dials + the token it must echo (ward#391).
func (r *Runner) startHostDispatchBroker(ctx context.Context, requester string) (addr, token string, cleanup func(), err error) {
	token, err = newDispatchBrokerToken()
	if err != nil {
		return "", "", func() {}, fmt.Errorf("ward dispatch broker: mint token: %w", err)
	}
	// Bind all interfaces: the container reaches the host via the docker gateway,
	// and loopback is unreachable from the LinuxKit VM. The token guards it.
	ln, err := net.Listen("tcp", dispatchBrokerListenAddr) //nolint:gosec // gateway-reachable bind, guarded by the per-launch token
	if err != nil {
		return "", "", func() {}, fmt.Errorf("ward dispatch broker: listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	addr = fmt.Sprintf("%s:%d", containerHostGateway, port)
	go r.serveHostDispatchBroker(ctx, ln, requester, token)
	return addr, token, func() {}, nil
}

// newDispatchBrokerToken mints a 256-bit hex nonce as the per-launch shared
// secret guarding the TCP transport (no socket file perm to lean on).
func newDispatchBrokerToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (r *Runner) serveHostDispatchBroker(ctx context.Context, ln net.Listener, requester, token string) {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Fprintf(os.Stderr, "ward dispatch broker: accept: %v\n", err)
			continue
		}
		go r.handleHostDispatchBrokerConn(ctx, conn, requester, token)
	}
}

func (r *Runner) handleHostDispatchBrokerConn(ctx context.Context, conn net.Conn, requester, token string) {
	defer func() { _ = conn.Close() }()
	var req dispatchBrokerRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeDispatchBrokerResponse(conn, "", fmt.Errorf("decode request: %w", err))
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
		writeDispatchBrokerResponse(conn, "", errors.New("dispatch broker: token rejected"))
		return
	}
	if req.Requester == "" {
		req.Requester = requester
	}
	logPath, err := r.runHostDispatchBrokerRequest(ctx, req)
	writeDispatchBrokerResponse(conn, logPath, err)
}

func writeDispatchBrokerResponse(conn net.Conn, logPath string, err error) {
	resp := dispatchBrokerResponse{OK: err == nil, LogPath: logPath}
	if err != nil {
		resp.Error = err.Error()
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// runHostDispatchBrokerRequest serves one validated run in-process, redirecting its
// deploy output to a per-dispatch log so it can't corrupt the surface TUI (ward#389).
func (r *Runner) runHostDispatchBrokerRequest(ctx context.Context, req dispatchBrokerRequest) (string, error) {
	if err := validateDispatchBrokerRequest(req); err != nil {
		return "", err
	}
	logf, logPath, err := openDispatchLog(req, time.Now())
	if err != nil {
		// Fail loud rather than fall back to the TTY: a broken log dir must not
		// silently reroute the flood back onto the corrupted surface (ward#389).
		return "", fmt.Errorf("dispatch broker: open run log: %w", err)
	}
	defer func() { _ = logf.Close() }()
	restore := redirectStdioToLog(logf)
	defer restore()

	_, _ = fmt.Fprintf(logf, "ward dispatch broker: %s requested `ward agent %s`\n",
		emptyDefault(req.Requester, "unknown-container"), strings.Join(req.Argv, " "))
	switch req.Role {
	case "engineer":
		return logPath, agentEngineerCommand().Run(ctx, req.Argv)
	case "advisor":
		return logPath, agentAdvisorCommand().Run(ctx, req.Argv)
	default:
		return logPath, fmt.Errorf("role %q is not dispatchable", req.Role)
	}
}

// openDispatchLog creates ~/.ward/agent-logs/dispatch and opens the per-dispatch
// log file for req, stamped at now so re-dispatches of the same ref don't collide.
func openDispatchLog(req dispatchBrokerRequest, now time.Time) (*os.File, string, error) {
	dir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, dispatchLogName(req, now))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) // #nosec G304 -- ward-derived path under ~/.ward
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

// dispatchLogName builds a filesystem-safe per-dispatch basename: a UTC stamp (sortable,
// distinct re-dispatches) plus a requester + ref slug (attributable). Pure, for testing.
func dispatchLogName(req dispatchBrokerRequest, now time.Time) string {
	ref := ""
	if len(req.Argv) >= 2 {
		ref = req.Argv[1]
	}
	slug := config.SanitizeSlug(emptyDefault(req.Requester, "unknown") + "-" + ref)
	return fmt.Sprintf("%s-%s.log", now.UTC().Format("20060102T150405Z"), slug)
}

// redirectStdioToLog swaps process os.Stdout/os.Stderr to logf for one served run (read
// at run time by its newRunner + subprocesses), serialized by dispatchStdioMu (ward#389).
func redirectStdioToLog(logf *os.File) func() {
	dispatchStdioMu.Lock()
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = logf, logf
	return func() {
		os.Stdout, os.Stderr = prevOut, prevErr
		dispatchStdioMu.Unlock()
	}
}

func validateDispatchBrokerRequest(req dispatchBrokerRequest) error {
	if req.Role != "engineer" && req.Role != "advisor" {
		return fmt.Errorf("dispatch broker: role %q refused (allowed: engineer, advisor)", req.Role)
	}
	if len(req.Argv) == 0 || req.Argv[0] != req.Role {
		return fmt.Errorf("dispatch broker: argv must begin with role %q", req.Role)
	}
	if len(req.Argv) < 2 {
		return fmt.Errorf("dispatch broker: missing issue ref")
	}
	for _, arg := range req.Argv {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("dispatch broker: argv contains NUL")
		}
	}
	if _, err := parseAgentIssueRef(req.Argv[1]); err != nil {
		return fmt.Errorf("dispatch broker: %s dispatch requires an issue ref, got %q", req.Role, req.Argv[1])
	}
	if req.Role == "advisor" && len(req.Argv) < 3 {
		return fmt.Errorf("dispatch broker: advisor dispatch requires a prompt after the issue ref")
	}
	return validateDispatchBrokerArgv(req.Role, req.Argv[2:])
}

func validateDispatchBrokerArgv(role string, tail []string) error {
	valueFlags := map[string]bool{"--driver": true}
	boolFlags := map[string]bool{"--print": true}
	if role == "engineer" {
		for _, f := range []string{"--image", "--tag", "--ward-version", "--branch", "--repo", "--tailnet-mode"} {
			valueFlags[f] = true
		}
		for _, f := range []string{"--aws", "--tailnet", "--no-pull", "--force", "--no-preflight"} {
			boolFlags[f] = true
		}
		return validateDispatchBrokerFlags(role, tail, valueFlags, boolFlags, false)
	}
	valueFlags["--thoroughness"] = true
	valueFlags["--depth"] = true
	return validateDispatchBrokerFlags(role, tail, valueFlags, boolFlags, true)
}

func validateDispatchBrokerFlags(role string, tail []string, valueFlags, boolFlags map[string]bool, allowPrompt bool) error {
	for i := 0; i < len(tail); i++ {
		arg := tail[i]
		if !strings.HasPrefix(arg, "-") {
			if allowPrompt {
				return nil
			}
			return fmt.Errorf("dispatch broker: %s argument %q refused after issue ref", role, arg)
		}
		if valueFlags[arg] {
			i++
			if i >= len(tail) || tail[i] == "" {
				return fmt.Errorf("dispatch broker: %s flag %s needs a value", role, arg)
			}
			continue
		}
		if boolFlags[arg] {
			continue
		}
		return fmt.Errorf("dispatch broker: %s flag %s is not approved", role, arg)
	}
	if allowPrompt {
		return fmt.Errorf("dispatch broker: advisor dispatch requires a prompt after the issue ref")
	}
	return nil
}

func emptyDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// maybeForwardAgentDispatchToHostBroker is the in-container ref-mode gate.
// It only runs inside a read-only director surface with a broker socket.
func (r *Runner) maybeForwardAgentDispatchToHostBroker(ctx context.Context, c *cli.Command, role string, mode containerMode) (bool, error) {
	addr := strings.TrimSpace(os.Getenv(envDispatchBrokerAddr))
	if addr == "" || os.Getenv("WARD_READONLY") != "1" {
		return false, nil
	}
	var argv []string
	switch role {
	case "engineer":
		ref, ok := r.brokerDispatchRef(ctx, c.Args().First())
		if !ok {
			return false, nil
		}
		argv = brokerEngineerArgv(c, mode, ref)
	case "advisor":
		ref, ok := r.brokerDispatchRef(ctx, c.Args().First())
		if !ok || len(c.Args().Tail()) == 0 {
			return false, nil
		}
		argv = brokerAdvisorArgv(c, mode, ref)
	default:
		return false, nil
	}
	req := dispatchBrokerRequest{
		Role:      role,
		Argv:      argv,
		Requester: strings.TrimSpace(os.Getenv("WARD_CONTAINER_NAME")),
		Token:     strings.TrimSpace(os.Getenv(envDispatchBrokerToken)),
	}
	logPath, err := sendDispatchBrokerRequest(ctx, addr, req)
	if err != nil {
		return true, err
	}
	// This line is captured as tool output by the surface agent, not written to the
	// raw TTY, so naming the host-side run log here is safe and aids discovery.
	if logPath != "" {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: forwarded `ward agent %s` to host ward (run output on the host at %s)\n",
			strings.Join(argv, " "), logPath)
	} else {
		fmt.Fprintf(os.Stderr, "ward dispatch broker: forwarded `ward agent %s` to host ward\n", strings.Join(argv, " "))
	}
	return true, nil
}

func (r *Runner) brokerDispatchRef(ctx context.Context, arg string) (agentIssueRef, bool) {
	ref, err := r.resolveAgentIssueRef(ctx, arg)
	if err != nil {
		return agentIssueRef{}, false
	}
	return ref, true
}

func brokerEngineerArgv(c *cli.Command, mode containerMode, ref agentIssueRef) []string {
	argv := []string{"engineer", ref.String(), "--driver", string(mode)}
	argv = appendBrokerContainerFlags(argv, c)
	if c.Bool("force") {
		argv = append(argv, "--force")
	}
	if c.Bool("no-preflight") {
		argv = append(argv, "--no-preflight")
	}
	if c.Bool("print") {
		argv = append(argv, "--print")
	}
	return argv
}

func brokerAdvisorArgv(c *cli.Command, mode containerMode, ref agentIssueRef) []string {
	argv := []string{"advisor", ref.String(), "--driver", string(mode)}
	if lvl := strings.TrimSpace(c.String("thoroughness")); lvl != "" {
		argv = append(argv, "--thoroughness", lvl)
	}
	if c.Bool("print") {
		argv = append(argv, "--print")
	}
	argv = append(argv, c.Args().Tail()...)
	return argv
}

func appendBrokerContainerFlags(argv []string, c *cli.Command) []string {
	for _, name := range []string{"image", "tag", "ward-version", "branch", "tailnet-mode"} {
		if v := strings.TrimSpace(c.String(name)); c.IsSet(name) && v != "" {
			argv = append(argv, "--"+name, v)
		}
	}
	for _, repo := range extraRepoGrant(c) {
		if repo = strings.TrimSpace(repo); repo != "" {
			argv = append(argv, "--repo", repo)
		}
	}
	for _, name := range []string{"aws", "tailnet", "no-pull"} {
		if c.Bool(name) {
			argv = append(argv, "--"+name)
		}
	}
	return argv
}

func sendDispatchBrokerRequest(ctx context.Context, addr string, req dispatchBrokerRequest) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		// Papercut #1 (ward#382): fail loud - name the transport + addr so an
		// unreachable host dispatch broker never reads as a bare dial error.
		return "", fmt.Errorf("%w: the host dispatch broker did not answer at %s "+
			"(WARD_DISPATCH_BROKER_ADDR, TCP over the docker gateway - see ward#382): %w",
			errDispatchBrokerUnavailable, addr, err)
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", fmt.Errorf("dispatch broker: send request: %w", err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return "", fmt.Errorf("dispatch broker: read response from %s: %w", addr, err)
	}
	if !resp.OK {
		// Papercut #2 (ward#382): the credential broker answers a dispatch dial with a
		// protocol-version refusal - surface it as a "wrong broker" hint, not a bare string.
		if isCredentialBrokerReply(resp.Error) {
			return "", fmt.Errorf("%w: %s answered as the credential broker, not the dispatch broker "+
				"(WARD_DISPATCH_BROKER_ADDR points at the wrong broker - see ward#382)",
				errDispatchBrokerUnavailable, addr)
		}
		return resp.LogPath, fmt.Errorf("dispatch broker: %s", resp.Error)
	}
	return resp.LogPath, nil
}

// isCredentialBrokerReply spots the credential broker's protocol-version refusal:
// the dispatch client reached cmd/ward/broker.go, not the dispatch broker (ward#382).
func isCredentialBrokerReply(msg string) bool {
	return strings.Contains(msg, "unsupported protocol version")
}
