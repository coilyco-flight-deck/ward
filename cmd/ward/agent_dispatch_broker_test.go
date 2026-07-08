package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

func TestDispatchBrokerValidatesNarrowAPI(t *testing.T) {
	for _, req := range []dispatchBrokerRequest{
		{Role: "exec", Argv: []string{"exec", "test"}},
		{Role: "engineer", Argv: []string{"exec", "test"}},
		{Role: "advisor", Argv: []string{"advisor"}},
		{Role: "advisor", Argv: []string{"advisor", "write me a poem"}},
		{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--ward-source", "/tmp/ward"}},
		{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "bad\x00arg"}},
	} {
		if err := validateDispatchBrokerRequest(req); err == nil {
			t.Errorf("validateDispatchBrokerRequest(%+v) = nil, want refusal", req)
		}
	}
	ok := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--harness", "claude"}}
	if err := validateDispatchBrokerRequest(ok); err != nil {
		t.Errorf("valid engineer dispatch refused: %v", err)
	}
	advisor := dispatchBrokerRequest{Role: "advisor", Argv: []string{"advisor", "coilyco-flight-deck/ward#1", "--harness", "goose", "what changed?"}}
	if err := validateDispatchBrokerRequest(advisor); err != nil {
		t.Errorf("valid advisor dispatch refused: %v", err)
	}
	// --agent is an equal first-class spelling of --harness (ward#660).
	equal := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--agent", "claude"}}
	if err := validateDispatchBrokerRequest(equal); err != nil {
		t.Errorf("--agent dispatch refused: %v", err)
	}
	// A pre-#660 container still writes --driver; the alias stays approved for one release.
	alias := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--driver", "claude"}}
	if err := validateDispatchBrokerRequest(alias); err != nil {
		t.Errorf("deprecated --driver dispatch refused: %v", err)
	}
	// --config is an approved repeatable value flag on both roles (ward#616).
	cfg := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--config", "agent.claude.model=sonnet"}}
	if err := validateDispatchBrokerRequest(cfg); err != nil {
		t.Errorf("valid engineer --config dispatch refused: %v", err)
	}
}

// TestDispatchBrokerValidatesStopShape locks the ward#627 stop protocol: a valid
// target with no argv passes; a bad target, argv on a stop, or a flag is refused.
func TestDispatchBrokerValidatesStopShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{"no target", dispatchBrokerRequest{Action: "stop"}},
		{"empty target", dispatchBrokerRequest{Action: "stop", Target: "  "}},
		{"stop carries launch argv", dispatchBrokerRequest{Action: "stop", Target: "coilyco-flight-deck/ward#1", Argv: []string{"engineer", "x"}}},
		{"flag target", dispatchBrokerRequest{Action: "stop", Target: "--force"}},
		{"url target", dispatchBrokerRequest{Action: "stop", Target: "https://example.com/x"}},
		{"metachar target", dispatchBrokerRequest{Action: "stop", Target: "name;rm -rf"}},
		{"launch carries a stop target", dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1"}, Target: "x"}},
		{"unknown action", dispatchBrokerRequest{Action: "nuke", Target: "x"}},
	} {
		if err := validateDispatchBrokerRequest(tc.req); err == nil {
			t.Errorf("%s: validateDispatchBrokerRequest(%+v) = nil, want refusal", tc.name, tc.req)
		}
	}
	for _, tc := range []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{"issue-ref target", dispatchBrokerRequest{Action: "stop", Target: "coilyco-flight-deck/ward#625"}},
		{"container-name target", dispatchBrokerRequest{Action: "stop", Target: "engineer-claude-ward-625"}},
	} {
		if err := validateDispatchBrokerRequest(tc.req); err != nil {
			t.Errorf("%s: valid stop refused: %v", tc.name, err)
		}
	}
}

// TestStopTargetGuardEngineerOnly is the ward#627 handler guard: only an engineer is
// stoppable; advisor/director/session are refused by role, and an empty role closed.
func TestStopTargetGuardEngineerOnly(t *testing.T) {
	if err := stopTargetGuard("engineer-claude-ward-625", roleEngineer); err != nil {
		t.Errorf("engineer target refused: %v", err)
	}
	for _, role := range []string{roleAdvisor, roleDirector, roleSession} {
		err := stopTargetGuard("some-"+role+"-box", role)
		if err == nil {
			t.Errorf("role %q was not refused", role)
		} else if !strings.Contains(err.Error(), role) {
			t.Errorf("refusal for role %q did not name the role: %v", role, err)
		}
	}
	if err := stopTargetGuard("mystery-box", ""); err == nil {
		t.Error("empty role did not fail closed")
	}
}

// TestSelectSingleStopTarget covers the ward#627 match-count rule: exactly one match
// stops, zero and more-than-one refuse (the multi case lists the candidates).
func TestSelectSingleStopTarget(t *testing.T) {
	got, err := selectSingleStopTarget("coilyco-flight-deck/ward#1", []string{"engineer-claude-ward-1"})
	if err != nil || got != "engineer-claude-ward-1" {
		t.Errorf("single match = (%q, %v), want the one name", got, err)
	}
	if _, err := selectSingleStopTarget("coilyco-flight-deck/ward#1", nil); err == nil {
		t.Error("zero matches did not refuse")
	}
	_, err = selectSingleStopTarget("coilyco-flight-deck/ward#1", []string{"engineer-claude-ward-1", "engineer-codex-ward-1"})
	if err == nil {
		t.Fatal("more-than-one match did not refuse")
	}
	for _, name := range []string{"engineer-claude-ward-1", "engineer-codex-ward-1"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("ambiguous refusal did not list candidate %q: %v", name, err)
		}
	}
}

// TestForwardAgentStopOffSurfaceErrors locks the ward#627 surface gate: with no
// dispatch broker addr set, stop errors rather than silently no-opping.
func TestForwardAgentStopOffSurfaceErrors(t *testing.T) {
	t.Setenv(envDispatchBrokerAddr, "")
	t.Setenv("WARD_READONLY", "")
	err := (&Runner{}).forwardAgentStopToHostBroker(context.Background(), "coilyco-flight-deck/ward#625")
	if err == nil {
		t.Fatal("off-surface stop did not error")
	}
	if !strings.Contains(err.Error(), "director read-only surface") {
		t.Errorf("off-surface error did not name the surface requirement: %v", err)
	}
}

// TestForwardAgentStopSendsStopRequest checks the ward#627 wire: a surface stop dials
// the broker with Action=stop + the target, and prints the stopped name it returns.
func TestForwardAgentStopSendsStopRequest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req dispatchBrokerRequest
		_ = json.NewDecoder(conn).Decode(&req)
		gotReq <- req
		// Echo the stopped container name back in the log-path slot.
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, LogPath: "engineer-claude-ward-625"})
	}()

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-627")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "session-claude-host")
	if err := (&Runner{}).forwardAgentStopToHostBroker(t.Context(), "coilyco-flight-deck/ward#625"); err != nil {
		t.Fatalf("forward stop: %v", err)
	}
	req := <-gotReq
	if req.Action != dispatchActionStop {
		t.Errorf("action = %q, want stop", req.Action)
	}
	if req.Target != "coilyco-flight-deck/ward#625" {
		t.Errorf("target = %q, want the ref", req.Target)
	}
	if req.Token != "nonce-627" {
		t.Errorf("token = %q, want the per-launch nonce", req.Token)
	}
	if len(req.Argv) != 0 {
		t.Errorf("stop request carried launch argv: %v", req.Argv)
	}
}

func TestBrokerEngineerArgvForwardsApprovedFlags(t *testing.T) {
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42",
		"--harness", "claude",
		"--image", "img", "--tag", "t1", "--ward-version", "v1",
		"--repo", "coilyco-flight-deck/cli-guard",
		"--config", "agent.claude.model=sonnet",
		"--aws", "--tailnet", "--tailnet-mode", "sidecar", "--force", "--no-preflight",
	})
	got := brokerEngineerArgv(cmd, modeClaude, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42})
	for _, want := range [][]string{
		{"--harness", "claude"},
		{"--image", "img"},
		{"--tag", "t1"},
		{"--ward-version", "v1"},
		{"--repo", "coilyco-flight-deck/cli-guard"},
		{"--config", "agent.claude.model=sonnet"},
		{"--tailnet-mode", "sidecar"},
	} {
		if !argFollowedBy(got, want[0], want[1]) {
			t.Errorf("forwarded argv missing %s %s: %v", want[0], want[1], got)
		}
	}
	for _, want := range []string{"engineer", "coilyco-flight-deck/ward#42", "--aws", "--tailnet", "--force", "--no-preflight"} {
		if !containsArg(got, want) {
			t.Errorf("forwarded argv missing %q: %v", want, got)
		}
	}
}

func TestForwardAgentDispatchToHostBrokerSendsCanonicalRequest(t *testing.T) {
	// ward#391: the transport is TCP over the docker gateway, not a unix socket, so
	// the stub broker listens on a loopback TCP port and the container dials it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req dispatchBrokerRequest
		_ = json.NewDecoder(conn).Decode(&req)
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
	}()

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-123")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "session-codex-host")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#378", "--harness", "claude", "--no-preflight",
	})
	forwarded, err := (&Runner{}).maybeForwardAgentDispatchToHostBroker(t.Context(), cmd, "engineer", modeClaude)
	if err != nil {
		t.Fatalf("forward dispatch: %v", err)
	}
	if !forwarded {
		t.Fatal("dispatch did not forward despite broker env")
	}
	req := <-gotReq
	if req.Role != "engineer" || req.Requester != "session-codex-host" {
		t.Fatalf("request identity = role %q requester %q", req.Role, req.Requester)
	}
	if req.Token != "nonce-123" {
		t.Errorf("forwarded token = %q, want the per-launch nonce", req.Token)
	}
	want := []string{"engineer", "coilyco-flight-deck/ward#378", "--harness", "claude", "--no-preflight"}
	if !reflect.DeepEqual(req.Argv, want) {
		t.Errorf("forwarded argv = %v, want %v", req.Argv, want)
	}
}

func TestDispatchBrokerEnvIsPlanLocal(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()[envDispatchBrokerAddr]; ok {
		t.Fatal("direct host dispatch plan unexpectedly has a dispatch broker addr env")
	}
	p.DispatchBrokerAddr = containerHostGateway + ":54321"
	p.DispatchBrokerToken = "nonce-abc"
	env := p.wardEnv()
	if got := env[envDispatchBrokerAddr]; got != containerHostGateway+":54321" {
		t.Errorf("broker addr env = %q, want %q", got, containerHostGateway+":54321")
	}
	if got := env[envDispatchBrokerToken]; got != "nonce-abc" {
		t.Errorf("broker token env = %q, want nonce-abc", got)
	}
}

// TestDispatchBrokerAddHostWiredForSurface locks the ward#391 Linux fallback: a
// surface plan wires --add-host, a plain plan does not (see the mapping below).
func TestDispatchBrokerAddHostWiredForSurface(t *testing.T) {
	p := sampleUpPlan()
	if containsArg(dockerCreateArgv(p, ""), "--add-host") {
		t.Fatal("plain plan unexpectedly wires --add-host")
	}
	p.DispatchBrokerAddr = containerHostGateway + ":1"
	argv := dockerCreateArgv(p, "")
	if !argFollowedBy(argv, "--add-host", containerHostGateway+":host-gateway") {
		t.Errorf("surface plan missing --add-host mapping: %v", argv)
	}
}

// TestDispatchBrokerTokenGate covers the auth the TCP port leans on: a mismatched
// token is refused before dispatch, a matching one reaches validation (ward#391).
func TestDispatchBrokerTokenGate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		token   string
		wantSub string
	}{
		{"mismatched token rejected", "wrong", "token rejected"},
		{"matching token reaches validation", "secret", "refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			go (&Runner{}).handleHostDispatchBrokerConn(context.Background(), server, "host", "secret")
			// Role "nope" fails validation, so a matching token stops at a validation
			// error ("refused") - proving it passed the token gate without dispatching.
			_ = json.NewEncoder(client).Encode(dispatchBrokerRequest{Role: "nope", Argv: []string{"nope"}, Token: tc.token})
			var resp dispatchBrokerResponse
			if err := json.NewDecoder(client).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			_ = client.Close()
			if resp.OK {
				t.Fatal("expected a refusal, got OK")
			}
			if !strings.Contains(resp.Error, tc.wantSub) {
				t.Errorf("error = %q, want contains %q", resp.Error, tc.wantSub)
			}
		})
	}
}

func TestNoBrokerKeepsDirectDispatchPath(t *testing.T) {
	t.Setenv(envDispatchBrokerAddr, "")
	t.Setenv("WARD_READONLY", "")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", "coilyco-flight-deck/ward#1"})
	forwarded, err := (&Runner{}).maybeForwardAgentDispatchToHostBroker(context.Background(), cmd, "engineer", modeClaude)
	if err != nil {
		t.Fatalf("unexpected direct-dispatch error: %v", err)
	}
	if forwarded {
		t.Fatal("direct host dispatch should not forward without broker env")
	}
}

// TestDispatchBrokerUnreachableFailsLoud locks papercut #1 (ward#382): an addr with
// nothing listening errors with errDispatchBrokerUnavailable and names the addr.
func TestDispatchBrokerUnreachableFailsLoud(t *testing.T) {
	// Bind then immediately close to get an addr guaranteed to refuse the dial.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	_, err = sendDispatchBrokerRequest(t.Context(), addr, dispatchBrokerRequest{Role: "engineer"})
	if err == nil {
		t.Fatal("dial to a closed addr unexpectedly succeeded")
	}
	if !errors.Is(err, errDispatchBrokerUnavailable) {
		t.Errorf("error = %v, want errors.Is errDispatchBrokerUnavailable", err)
	}
	if !strings.Contains(err.Error(), addr) {
		t.Errorf("error %q does not name the addr %q", err, addr)
	}
}

// TestDispatchBrokerWrongBrokerHint locks papercut #2 (ward#382): a dial that reaches
// the credential broker (a protocol-version refusal) surfaces a "wrong broker" hint.
func TestDispatchBrokerWrongBrokerHint(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req dispatchBrokerRequest
		_ = json.NewDecoder(conn).Decode(&req)
		// Mimic the credential broker refusing the dispatch protocol handshake.
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{
			OK:    false,
			Error: "unsupported protocol version 0 (want 1)",
		})
	}()

	_, err = sendDispatchBrokerRequest(t.Context(), ln.Addr().String(), dispatchBrokerRequest{Role: "engineer"})
	if err == nil {
		t.Fatal("credential-broker reply unexpectedly accepted")
	}
	if !strings.Contains(err.Error(), "wrong broker") {
		t.Errorf("error %q does not carry the wrong-broker hint", err)
	}
}

// TestDispatchLogNameIsStampedAndAttributable locks the ward#389 log basename: a UTC
// minute stamp for sortable re-dispatches plus a filesystem-safe requester + ref slug.
func TestDispatchLogNameIsStampedAndAttributable(t *testing.T) {
	at := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	req := dispatchBrokerRequest{
		Requester: "session-claude-ward-x",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#389", "--driver", "claude"},
	}
	got := dispatchLogName(req, at)
	want := "20260701T120000Z-session-claude-ward-x-coilyco-flight-deck-ward-389.log"
	if got != want {
		t.Errorf("dispatchLogName() = %q, want %q", got, want)
	}
	// A requester-less request still yields a sane, collision-free basename.
	bare := dispatchLogName(dispatchBrokerRequest{Argv: []string{"advisor"}}, at)
	if !strings.HasPrefix(bare, "20260701T120000Z-unknown") || !strings.HasSuffix(bare, ".log") {
		t.Errorf("requester-less dispatchLogName() = %q, want stamped unknown-*.log", bare)
	}
}

// TestServedRunStdioLandsInLogNotTTY is the ward#389 regression: the redirect routes a
// served run's os.Stdout/os.Stderr bytes into the per-dispatch log, then restores them.
func TestServedRunStdioLandsInLogNotTTY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := dispatchBrokerRequest{
		Requester: "session-claude-ward-1",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1", "--driver", "claude"},
	}
	logf, logPath, err := openDispatchLog(req, time.Now())
	if err != nil {
		t.Fatalf("openDispatchLog: %v", err)
	}
	if want := filepath.Join(agentLogsDir(), dispatchLogsSubdir); !strings.HasPrefix(logPath, want) {
		t.Errorf("log path %q not under %q", logPath, want)
	}

	origOut, origErr := os.Stdout, os.Stderr
	restore := redirectStdioToLog(logf)
	if os.Stdout != logf || os.Stderr != logf {
		restore()
		_ = logf.Close()
		t.Fatal("redirect did not point os.Stdout/os.Stderr at the log file")
	}
	// A byte a served run would emit lands in the log, not on the terminal.
	fmt.Fprint(os.Stderr, "session-claude-ward-1: pulling some-image\n")
	restore()
	_ = logf.Close()

	if os.Stdout != origOut || os.Stderr != origErr {
		t.Fatal("restore did not put os.Stdout/os.Stderr back")
	}
	body, err := os.ReadFile(logPath) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(body), "pulling some-image") {
		t.Errorf("run output did not land in the log; got %q", body)
	}
}

func parseCommandForTest(t *testing.T, flags []cli.Flag, argv []string) *cli.Command {
	t.Helper()
	cmd := &cli.Command{Name: argv[0], Flags: flags, Action: func(context.Context, *cli.Command) error { return nil }}
	if err := cmd.Run(t.Context(), argv); err != nil {
		t.Fatalf("parse %s: %v", strings.Join(argv, " "), err)
	}
	return cmd
}
