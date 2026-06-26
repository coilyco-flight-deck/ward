package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

// agent_dispatch_broker.go wires ward#378: director surfaces ask host ward to
// launch sibling engineer/advisor runs through a narrow socket API.

const envDispatchBrokerSocket = "WARD_DISPATCH_BROKER_SOCK"

var errDispatchBrokerUnavailable = errors.New("dispatch broker unavailable")

type dispatchBrokerRequest struct {
	Role      string   `json:"role"`
	Argv      []string `json:"argv"`
	Requester string   `json:"requester,omitempty"`
}

type dispatchBrokerResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// startHostDispatchBroker serves validated dispatch requests until ctx ends.
// The returned host socket is mounted into the director surface container.
func (r *Runner) startHostDispatchBroker(ctx context.Context, requester string) (socket string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "ward-dispatch-broker-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("ward dispatch broker: create socket dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	socket = filepath.Join(dir, "broker.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("ward dispatch broker: listen: %w", err)
	}
	go r.serveHostDispatchBroker(ctx, ln, requester)
	return socket, cleanup, nil
}

func (r *Runner) serveHostDispatchBroker(ctx context.Context, ln net.Listener, requester string) {
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
		go r.handleHostDispatchBrokerConn(ctx, conn, requester)
	}
}

func (r *Runner) handleHostDispatchBrokerConn(ctx context.Context, conn net.Conn, requester string) {
	defer func() { _ = conn.Close() }()
	var req dispatchBrokerRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeDispatchBrokerResponse(conn, fmt.Errorf("decode request: %w", err))
		return
	}
	if req.Requester == "" {
		req.Requester = requester
	}
	err := r.runHostDispatchBrokerRequest(ctx, req)
	writeDispatchBrokerResponse(conn, err)
}

func writeDispatchBrokerResponse(conn net.Conn, err error) {
	resp := dispatchBrokerResponse{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func (r *Runner) runHostDispatchBrokerRequest(ctx context.Context, req dispatchBrokerRequest) error {
	if err := validateDispatchBrokerRequest(req); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: %s requested `ward agent %s`\n",
		emptyDefault(req.Requester, "unknown-container"), strings.Join(req.Argv, " "))
	switch req.Role {
	case "engineer":
		return agentEngineerCommand().Run(ctx, req.Argv)
	case "advisor":
		return agentAdvisorCommand().Run(ctx, req.Argv)
	default:
		return fmt.Errorf("role %q is not dispatchable", req.Role)
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
		for _, f := range []string{"--image", "--tag", "--ward-version", "--branch", "--repo", "--with-repo"} {
			valueFlags[f] = true
		}
		for _, f := range []string{"--aws", "--host-net", "--ts-sidecar", "--no-pull", "--go-bootstrap", "--force", "--no-preflight"} {
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
	sock := strings.TrimSpace(os.Getenv(envDispatchBrokerSocket))
	if sock == "" || os.Getenv("WARD_READONLY") != "1" {
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
	}
	if err := sendDispatchBrokerRequest(ctx, sock, req); err != nil {
		return true, err
	}
	fmt.Fprintf(os.Stderr, "ward dispatch broker: forwarded `ward agent %s` to host ward\n", strings.Join(argv, " "))
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
	for _, name := range []string{"image", "tag", "ward-version", "branch"} {
		if v := strings.TrimSpace(c.String(name)); c.IsSet(name) && v != "" {
			argv = append(argv, "--"+name, v)
		}
	}
	for _, repo := range c.StringSlice("with-repo") {
		if repo = strings.TrimSpace(repo); repo != "" {
			argv = append(argv, "--repo", repo)
		}
	}
	for _, name := range []string{"aws", "host-net", "ts-sidecar", "no-pull", "go-bootstrap"} {
		if c.Bool(name) {
			argv = append(argv, "--"+name)
		}
	}
	return argv
}

func sendDispatchBrokerRequest(ctx context.Context, socket string, req dispatchBrokerRequest) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("%w: dial %s: %w", errDispatchBrokerUnavailable, socket, err)
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("dispatch broker: send request: %w", err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("dispatch broker: read response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("dispatch broker: %s", resp.Error)
	}
	return nil
}

func dispatchBrokerMount(hostSocket string) mountSpec {
	return mountSpec{Source: hostSocket, Target: containerDispatchBrokerSock, ReadOnly: false, Volume: false}
}
