package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/urfave/cli/v3"
)

func serveDispatchBrokerRequests(t *testing.T, ln net.Listener, handle func(net.Conn, dispatchBrokerRequest)) {
	t.Helper()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				var req dispatchBrokerRequest
				if err := json.NewDecoder(conn).Decode(&req); err != nil {
					return
				}
				handle(conn, req)
			}()
		}
	}()
}

func roundTripDispatchBrokerRequest(t *testing.T, addr string, req dispatchBrokerRequest) dispatchBrokerResponse {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial broker %s: %v", addr, err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	var resp dispatchBrokerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestDispatchBrokerValidatesNarrowAPI(t *testing.T) {
	for _, req := range []dispatchBrokerRequest{
		{Role: "exec", Argv: []string{"exec", "test"}},
		{Role: "engineer", Argv: []string{"exec", "test"}},
		{Role: "advisor", Argv: []string{"advisor"}},
		{Role: "advisor", Argv: []string{"advisor", "write me a poem"}},
		{Role: "advisor", Argv: []string{"advisor", "coilyco-flight-deck/ward#1", "--harness", "goose"}},
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
	pr := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--pr", "--harness", "claude"}}
	if err := validateDispatchBrokerRequest(pr); err != nil {
		t.Errorf("valid engineer PR dispatch refused: %v", err)
	}
	qa := dispatchBrokerRequest{Role: "qa", Argv: []string{"qa", "coilyco-flight-deck/ward#1", "--harness", "claude", "inspect the branch"}}
	if err := validateDispatchBrokerRequest(qa); err != nil {
		t.Errorf("valid qa dispatch refused: %v", err)
	}
	// --agent is an equal first-class spelling of --harness (ward#660).
	equal := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--agent", "claude"}}
	if err := validateDispatchBrokerRequest(equal); err != nil {
		t.Errorf("--agent dispatch refused: %v", err)
	}
	// --config is an approved repeatable value flag on both roles (ward#616).
	cfg := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--config", "agent.claude.model=sonnet"}}
	if err := validateDispatchBrokerRequest(cfg); err != nil {
		t.Errorf("valid engineer --config dispatch refused: %v", err)
	}
	wf := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--workflow", "merge-remote-main", "--details", "repair after PR #357"}}
	if err := validateDispatchBrokerRequest(wf); err != nil {
		t.Errorf("valid engineer --workflow/--details dispatch refused: %v", err)
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
		{"flag target", dispatchBrokerRequest{Action: "stop", Target: "--bogus"}},
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

// TestDispatchBrokerValidatesLogsShape locks the ward#694 logs protocol: a target
// with no argv passes; a bad target, argv on a logs request, or a flag is refused.
func TestDispatchBrokerValidatesLogsShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{"logs carries launch argv", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "coilyco-flight-deck/ward#1", Argv: []string{"engineer", "x"}}},
		{"flag target", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "--bogus"}},
		{"url target", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "https://example.com/x"}},
		{"metachar target", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "name;rm -rf"}},
		{"negative tail", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "engineer-claude-ward-1", Tail: -1}},
		{"launch carries a logs target", dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1"}, Target: "x"}},
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
		{"no target", dispatchBrokerRequest{Action: dispatchActionLogs}},
		{"empty target", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "  "}},
		{"issue-ref target", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "coilyco-flight-deck/ward#625"}},
		{"container-name target", dispatchBrokerRequest{Action: dispatchActionLogs, Target: "engineer-claude-ward-625"}},
	} {
		if err := validateDispatchBrokerRequest(tc.req); err != nil {
			t.Errorf("%s: valid logs request refused: %v", tc.name, err)
		}
	}
}

// TestDispatchBrokerValidatesListShape locks the ward#874 list protocol: no argv,
// no target, and a known format.
func TestDispatchBrokerValidatesListShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{"with argv", dispatchBrokerRequest{Action: dispatchActionList, Argv: []string{"list"}}},
		{"with target", dispatchBrokerRequest{Action: dispatchActionList, Target: "x"}},
		{"bad format", dispatchBrokerRequest{Action: dispatchActionList, Format: "yaml"}},
		{"launch carries list action", dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1"}, Action: dispatchActionList}},
	} {
		if err := validateDispatchBrokerRequest(tc.req); err == nil {
			t.Errorf("%s: validateDispatchBrokerRequest(%+v) = nil, want refusal", tc.name, tc.req)
		}
	}
	for _, tc := range []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{"text format", dispatchBrokerRequest{Action: dispatchActionList, Format: "text"}},
		{"json format", dispatchBrokerRequest{Action: dispatchActionList, Format: "json"}},
		{"default format", dispatchBrokerRequest{Action: dispatchActionList}},
	} {
		if err := validateDispatchBrokerRequest(tc.req); err != nil {
			t.Errorf("%s: valid list request refused: %v", tc.name, err)
		}
	}
}

// TestStopTargetGuardEngineerOnly is the ward#627 handler guard: only an engineer is
// stoppable; director/qa/session are refused by role, and an empty role closed.
func TestStopTargetGuardEngineerOnly(t *testing.T) {
	if err := stopTargetGuard("engineer-claude-ward-625", roleEngineer); err != nil {
		t.Errorf("engineer target refused: %v", err)
	}
	for _, role := range []string{roleDirector, roleQA, roleSession} {
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

func TestResolveEngineerStopTargetStopsVisibleEngineer(t *testing.T) {
	r := fakeStopDockRunner(t, "engineer-codex-ward-625")
	got, err := r.resolveEngineerStopTarget(t.Context(), "coilyco-flight-deck/ward#625")
	if err != nil {
		t.Fatalf("resolve visible engineer: %v", err)
	}
	if got != "engineer-codex-ward-625" {
		t.Fatalf("resolve visible engineer = %q, want the running container", got)
	}
}

func TestResolveEngineerStopTargetClassifiesStaleReservationForCleanup(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1200}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("reservation path: %v", err)
	}
	if err := writeAgentReservation(path, agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(modeCodex),
		Container: "engineer-codex-ward-1200",
		At:        time.Now().Add(-2 * agentLaunchConfirmationTTL()),
	}); err != nil {
		t.Fatalf("write reservation: %v", err)
	}
	r := fakeStopDockRunner(t, "")
	_, err = r.resolveEngineerStopTarget(t.Context(), ref.String())
	if err == nil {
		t.Fatal("stale reservation was accepted as a running container")
	}
	var stale *staleEngineerLaunchError
	if !errors.As(err, &stale) {
		t.Fatalf("stale reservation error = %T %v, want *staleEngineerLaunchError", err, err)
	}
	if stale.hold.Ref() != ref {
		t.Fatalf("stale reservation ref = %s, want %s", stale.hold.Ref(), ref)
	}
	got, err := r.runDispatchBrokerStopPreview(t.Context(), dispatchBrokerRequest{Action: dispatchActionStop, Target: ref.String(), Preview: true})
	if err != nil {
		t.Fatalf("preview stale cleanup: %v", err)
	}
	if want := staleLaunchCleanupResultPrefix + ref.String(); got != want {
		t.Fatalf("preview stale cleanup = %q, want %q", got, want)
	}
}

func TestResolveEngineerStopTargetRefusesFreshLaunchIntent(t *testing.T) {
	setTestHome(t, t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1201}
	path, err := agentReservationPath(ref)
	if err != nil {
		t.Fatalf("reservation path: %v", err)
	}
	if err := writeAgentReservation(path, agentReservation{
		Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number,
		Mode: string(modeCodex), Container: "engineer-codex-ward-1201", At: time.Now(),
	}); err != nil {
		t.Fatalf("write reservation: %v", err)
	}
	r := fakeStopDockRunner(t, "")
	_, err = r.resolveEngineerStopTarget(t.Context(), ref.String())
	if err == nil || !strings.Contains(err.Error(), "fresh launch intent") {
		t.Fatalf("fresh launch intent error = %v, want confirmation-window refusal", err)
	}
}

func TestResolveEngineerStopTargetUnknownRef(t *testing.T) {
	setTestHome(t, t.TempDir())
	r := fakeStopDockRunner(t, "")
	_, err := r.resolveEngineerStopTarget(t.Context(), "coilyco-flight-deck/ward#9999")
	if err == nil {
		t.Fatal("unknown ref was accepted as stoppable")
	}
	if !strings.Contains(err.Error(), "no running engineer container matches") {
		t.Fatalf("unknown ref refusal = %v, want the generic no-match message", err)
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
	err := (&Runner{}).forwardAgentStopToHostBroker(context.Background(), "coilyco-flight-deck/ward#625", false)
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
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		// Echo the stopped container name back in the log-path slot.
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, LogPath: "engineer-claude-ward-625"})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-627")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-claude-host")
	if err := (&Runner{}).forwardAgentStopToHostBroker(t.Context(), "coilyco-flight-deck/ward#625", false); err != nil {
		t.Fatalf("forward stop: %v", err)
	}
	req := <-gotReq
	if req.Action != dispatchActionStop {
		t.Errorf("action = %q, want stop", req.Action)
	}
	if req.Target != "coilyco-flight-deck/ward#625" {
		t.Errorf("target = %q, want the ref", req.Target)
	}
	if req.Preview {
		t.Error("stop request unexpectedly marked preview")
	}
	if req.Token != "nonce-627" {
		t.Errorf("token = %q, want the per-launch nonce", req.Token)
	}
	if len(req.Argv) != 0 {
		t.Errorf("stop request carried launch argv: %v", req.Argv)
	}
}

func TestForwardAgentStopPrintRequestsPreview(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, LogPath: "engineer-claude-ward-625"})
	})

	r := &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-627")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-claude-host")
	origStderr := os.Stderr
	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	t.Cleanup(func() {
		_ = wPipe.Close()
		_ = rPipe.Close()
		os.Stderr = origStderr
	})
	os.Stderr = wPipe
	cmd := &cli.Command{
		Name:  "stop",
		Flags: []cli.Flag{&cli.BoolFlag{Name: "print"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			return r.runAgentStop(ctx, c)
		},
	}
	if err := cmd.Run(t.Context(), []string{"stop", "--print", "coilyco-flight-deck/ward#625"}); err != nil {
		t.Fatalf("run stop --print: %v", err)
	}
	_ = wPipe.Close()
	req := <-gotReq
	if req.Action != dispatchActionStop {
		t.Errorf("action = %q, want stop", req.Action)
	}
	if !req.Preview {
		t.Error("print request was not marked preview")
	}
	if req.Target != "coilyco-flight-deck/ward#625" {
		t.Errorf("target = %q, want the ref", req.Target)
	}
	var stderr bytes.Buffer
	if _, err := io.Copy(&stderr, rPipe); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if !strings.Contains(stderr.String(), "would stop engineer container engineer-claude-ward-625 on host ward") {
		t.Fatalf("print output = %q, want the preview stop line", stderr.String())
	}
}

func TestRunAgentLogsWithoutTargetForwardsComposeGroupDefault(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, Source: "compose project ward-director-ward-codex (2 containers) --tail 100"})
		_, _ = io.WriteString(conn, "===== ward agent logs: director-codex-ward-1567 =====\nline\n")
	})

	r := &Runner{Runner: &shell.Runner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}}
	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-1567")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("run logs without target: %v", err)
	}
	req := <-gotReq
	if req.Action != dispatchActionLogs {
		t.Errorf("action = %q, want logs", req.Action)
	}
	if req.Target != "" {
		t.Errorf("target = %q, want empty compose-group request", req.Target)
	}
	if req.Tail != agentLogsDefaultGroupTail {
		t.Errorf("tail = %d, want %d", req.Tail, agentLogsDefaultGroupTail)
	}
	if req.Follow {
		t.Error("default group request should not follow")
	}
	if len(req.Argv) != 0 {
		t.Errorf("logs request carried launch argv: %v", req.Argv)
	}
	if !strings.Contains(r.Runner.Stderr.(*bytes.Buffer).String(), "compose project ward-director-ward-codex (2 containers) --tail 100") {
		t.Errorf("stderr did not name the compose group: %q", r.Runner.Stderr.(*bytes.Buffer).String())
	}
	if !strings.Contains(r.Runner.Stdout.(*bytes.Buffer).String(), "===== ward agent logs: director-codex-ward-1567 =====") {
		t.Errorf("stdout did not relay grouped log body: %q", r.Runner.Stdout.(*bytes.Buffer).String())
	}
}

func TestForwardAgentLogsSendsLogsRequestAndRelaysBody(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, Source: "docker logs engineer-claude-ward-625 --tail 2"})
		_, _ = io.WriteString(conn, "line-one\nline-two\n")
	})

	r := &Runner{Runner: &shell.Runner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}}
	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-694")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-claude-host")
	if err := r.forwardAgentLogsToHostBroker(t.Context(), ln.Addr().String(), "coilyco-flight-deck/ward#625", 2, false); err != nil {
		t.Fatalf("forward logs: %v", err)
	}
	req := <-gotReq
	if req.Action != dispatchActionLogs {
		t.Errorf("action = %q, want logs", req.Action)
	}
	if req.Target != "coilyco-flight-deck/ward#625" {
		t.Errorf("target = %q, want the ref", req.Target)
	}
	if req.Token != "nonce-694" {
		t.Errorf("token = %q, want the per-launch nonce", req.Token)
	}
	if req.Tail != 2 {
		t.Errorf("tail = %d, want 2", req.Tail)
	}
	if req.Follow {
		t.Error("snapshot logs request should not set follow")
	}
	if len(req.Argv) != 0 {
		t.Errorf("logs request carried launch argv: %v", req.Argv)
	}
	if out := r.Runner.Stdout.(*bytes.Buffer).String(); out != "line-one\nline-two\n" {
		t.Errorf("relayed logs = %q, want the streamed body", out)
	}
	if !strings.Contains(r.Runner.Stderr.(*bytes.Buffer).String(), "docker logs engineer-claude-ward-625 --tail 2") {
		t.Errorf("stderr did not name the source: %q", r.Runner.Stderr.(*bytes.Buffer).String())
	}
}

func TestForwardAgentListSendsListRequestAndRelaysBody(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
		_, _ = io.WriteString(conn, "ward agent: running engineer containers (1)\n")
	})

	r := &Runner{Runner: &shell.Runner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}}
	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-874")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-claude-host")
	if err := r.forwardAgentListToHostBroker(t.Context(), ln.Addr().String(), false); err != nil {
		t.Fatalf("forward list: %v", err)
	}
	req := <-gotReq
	if req.Action != dispatchActionList {
		t.Errorf("action = %q, want list", req.Action)
	}
	if req.Format != "text" {
		t.Errorf("format = %q, want text", req.Format)
	}
	if req.Token != "nonce-874" {
		t.Errorf("token = %q, want the per-launch nonce", req.Token)
	}
	if len(req.Argv) != 0 {
		t.Errorf("list request carried launch argv: %v", req.Argv)
	}
	if out := r.Runner.Stdout.(*bytes.Buffer).String(); out != "ward agent: running engineer containers (1)\n" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestResolveDispatchBrokerLogsSourcePrefersLiveDocker(t *testing.T) {
	r := fakeAgentLogsDockerRunner(t, "engineer-claude-ward-692\n", "live-one\nlive-two\n", nil, "")
	src, err := r.resolveDispatchBrokerLogsSource(t.Context(), dispatchBrokerRequest{Target: "coilyco-flight-deck/ward#692", Tail: 2})
	if err != nil {
		t.Fatalf("resolve live source: %v", err)
	}
	if got, want := src.String(), "docker logs engineer-claude-ward-692 --tail 2"; got != want {
		t.Errorf("live source = %q, want %q", got, want)
	}
	var out bytes.Buffer
	if err := r.streamAgentLogsSource(t.Context(), src, &out); err != nil {
		t.Fatalf("stream live source: %v", err)
	}
	if got := out.String(); got != "live-one\nlive-two\n" {
		t.Errorf("live stream = %q, want the docker output", got)
	}
}

func TestResolveDispatchBrokerLogsSourceFallsBackToLiveTranscriptWhenDockerEmpty(t *testing.T) {
	tarBytes := liveTranscriptTar(t, map[string]string{
		"projects/enc/session-a.jsonl": `{"type":"assistant","text":"working"}` + "\n" + `{"type":"assistant","text":"still here"}` + "\n",
		"projects/enc/session-b.jsonl": `{"type":"assistant","text":"latest"}` + "\n",
	})
	r := fakeAgentLogsDockerRunner(t, "engineer-claude-ward-692\n", "", tarBytes, ".claude/projects")
	src, err := r.resolveDispatchBrokerLogsSource(t.Context(), dispatchBrokerRequest{Target: "coilyco-flight-deck/ward#692", Tail: 2})
	if err != nil {
		t.Fatalf("resolve transcript source: %v", err)
	}
	var out bytes.Buffer
	if err := r.streamAgentLogsSource(t.Context(), src, &out); err != nil {
		t.Fatalf("stream transcript fallback: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"ward agent logs: docker logs engineer-claude-ward-692 --tail 2 had no readable bytes; using live transcript tree from /home/ubuntu/.ward/.claude/projects",
		".claude/projects",
		`{"type":"assistant","text":"still here"}`,
		`{"type":"assistant","text":"latest"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("fallback output missing %q\n%s", want, got)
		}
	}
}

func TestResolveDispatchBrokerLogsSourceUsesCodexTranscriptTreeWhenDockerEmpty(t *testing.T) {
	tarBytes := liveTranscriptTar(t, map[string]string{
		"sessions/session-a.jsonl": `{"type":"assistant","text":"working"}` + "\n" + `{"type":"assistant","text":"still here"}` + "\n",
		"sessions/session-b.jsonl": `{"type":"assistant","text":"latest"}` + "\n",
	})
	r := fakeAgentLogsDockerRunner(t, "engineer-codex-ward-692\n", "", tarBytes, ".codex/sessions")
	src, err := r.resolveDispatchBrokerLogsSource(t.Context(), dispatchBrokerRequest{Target: "coilyco-flight-deck/ward#692", Tail: 2})
	if err != nil {
		t.Fatalf("resolve transcript source: %v", err)
	}
	var out bytes.Buffer
	if err := r.streamAgentLogsSource(t.Context(), src, &out); err != nil {
		t.Fatalf("stream transcript fallback: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"ward agent logs: docker logs engineer-codex-ward-692 --tail 2 had no readable bytes; using live transcript tree from /home/ubuntu/.ward/.codex/sessions",
		".codex/sessions",
		`{"type":"assistant","text":"still here"}`,
		`{"type":"assistant","text":"latest"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("fallback output missing %q\n%s", want, got)
		}
	}
}

func TestResolveDispatchBrokerLogsSourceFallsBackToArchive(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	archiveDir := filepath.Join(home, ".ward", "agent-logs", "engineer-claude-ward-692")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	meta := runMeta{Container: "engineer-claude-ward-692", Repo: "coilyco-flight-deck/ward", Issue: "692", Outcome: outcomeUnknown}
	metaBody, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(archiveDir, drainMetaFile), metaBody, 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, drainConsoleFile), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write console: %v", err)
	}
	r := fakeAgentLogsDockerRunner(t, "", "", nil, "")
	src, err := r.resolveDispatchBrokerLogsSource(t.Context(), dispatchBrokerRequest{Target: "coilyco-flight-deck/ward#692", Tail: 1})
	if err != nil {
		t.Fatalf("resolve archive source: %v", err)
	}
	if src.Kind != agentLogSourceFile {
		t.Fatalf("archive source kind = %q, want file", src.Kind)
	}
	if got := src.Path; got != filepath.Join(archiveDir, drainConsoleFile) {
		t.Errorf("archive path = %q, want %q", got, filepath.Join(archiveDir, drainConsoleFile))
	}
	if got := src.String(); !strings.Contains(got, "(outcome unknown)") {
		t.Fatalf("archive source string = %q, want the final outcome marker", got)
	}
	var out bytes.Buffer
	if err := r.streamAgentLogsSource(t.Context(), src, &out); err != nil {
		t.Fatalf("stream archive source: %v", err)
	}
	if got := out.String(); got != "three\n" {
		t.Errorf("archive tail = %q, want the last line", got)
	}
}

func TestRunAgentLogsIssueScopedArchiveEmptyExplainsSelectedSource(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	archiveDir := filepath.Join(home, ".ward", "agent-logs", "engineer-claude-ward-692")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	meta := runMeta{Container: "engineer-claude-ward-692", Repo: "coilyco-flight-deck/ward", Issue: "692", Outcome: outcomeUnknown}
	metaBody, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(archiveDir, drainMetaFile), metaBody, 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, drainConsoleFile), nil, 0o644); err != nil {
		t.Fatalf("write empty console: %v", err)
	}

	r := fakeAgentLogsDockerRunner(t, "", "", nil, "")
	var stdout, stderr bytes.Buffer
	r.Runner.Stdout = &stdout
	r.Runner.Stderr = &stderr

	cmd := parseCommandForTest(t, agentLogsCommand().Flags, []string{"logs", "coilyco-flight-deck/ward#692"})
	if err := r.runAgentLogs(t.Context(), cmd); err != nil {
		t.Fatalf("runAgentLogs: %v", err)
	}

	if got := stderr.String(); !strings.Contains(got, "ward agent logs: using archive path ") {
		t.Fatalf("stderr = %q, want the selected archive path", got)
	}
	for _, want := range []string{
		"ward agent logs: archive path ",
		filepath.Join(archiveDir, drainConsoleFile),
		"has no readable bytes",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestBrokerEngineerArgvForwardsApprovedFlags(t *testing.T) {
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42",
		"--harness", "claude",
		"--image", "img", "--tag", "t1", "--ward-version", "v1",
		"--repo", "coilyco-flight-deck/cli-guard",
		"--config", "agent.claude.model=sonnet",
		"--workflow", "merge-remote-main", "--details", "repair after PR #357",
		"--skip-preflight", "--skip-smoke-test",
	})
	got := brokerEngineerArgv(cmd, modeClaude, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42})
	for _, want := range [][]string{
		{"--harness", "claude"},
		{"--image", "img"},
		{"--tag", "t1"},
		{"--ward-version", "v1"},
		{"--repo", "coilyco-flight-deck/cli-guard"},
		{"--config", "agent.claude.model=sonnet"},
		{"--workflow", "merge-remote-main"},
		{"--details", "repair after PR #357"},
	} {
		if !argFollowedBy(got, want[0], want[1]) {
			t.Errorf("forwarded argv missing %s %s: %v", want[0], want[1], got)
		}
	}
	for _, want := range []string{"engineer", forgejoBaseURL + "/coilyco-flight-deck/ward/issues/42", "--skip-preflight", "--skip-smoke-test"} {
		if !containsArg(got, want) {
			t.Errorf("forwarded argv missing %q: %v", want, got)
		}
	}
}

func TestBrokerEngineerArgvNormalizesSmokeSkipEnvironment(t *testing.T) {
	t.Setenv("WARD_SMOKE_TEST_SKIP", "1")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42", "--harness", "opencode",
	})
	got := brokerEngineerArgv(cmd, modeOpencode, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42})
	if !containsArg(got, "--skip-smoke-test") {
		t.Fatalf("brokered argv did not normalize WARD_SMOKE_TEST_SKIP=1: %v", got)
	}
}

func TestBrokerEngineerArgvNormalizesLocalHarnessEnvironment(t *testing.T) {
	t.Setenv("WARD_OPENCODE_MODEL", "deployment-model")
	t.Setenv("WARD_OLLAMA_URL", "http://deployment.example/v1")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42", "--harness", "opencode",
	})
	got := brokerEngineerArgv(cmd, modeOpencode, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42})
	for _, want := range []string{
		"agent.opencode.model=deployment-model",
		"agent.opencode.endpoint=http://deployment.example/v1",
	} {
		if !argFollowedBy(got, "--config", want) {
			t.Errorf("brokered argv missing explicit local config %q: %v", want, got)
		}
	}
}

func TestBrokerEngineerArgvKeepsRequestLocalConfigPrecedence(t *testing.T) {
	t.Setenv("WARD_GOOSE_MODEL", "inherited-model")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42", "--harness", "goose",
		"--config", "agent.goose.model=request-model",
	})
	got := brokerEngineerArgv(cmd, modeGoose, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42})
	if !argFollowedBy(got, "--config", "agent.goose.model=request-model") {
		t.Fatalf("brokered argv lost request-local config: %v", got)
	}
	if strings.Contains(strings.Join(got, " "), "inherited-model") {
		t.Fatalf("brokered argv appended inherited config over request-local config: %v", got)
	}
}

func TestBrokerEngineerArgvPreservesPullRequestRefs(t *testing.T) {
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward!42",
		"--harness", "claude",
		"--print",
	})
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42, MergeRequest: true}
	got := brokerEngineerArgv(cmd, modeClaude, ref)
	if !containsArg(got, forgejoBaseURL+"/coilyco-flight-deck/ward/pulls/42") {
		t.Fatalf("brokered argv should preserve PR refs, got %v", got)
	}
	if dispatchBrokerLaunchHasContinuationBranch(got) == false {
		t.Fatalf("brokered argv should be treated as continuation work, got %v", got)
	}
	if !containsArg(got, "--print") {
		t.Fatalf("brokered argv should preserve --print, got %v", got)
	}
}

// TestBrokerEngineerArgvForwardsOverrideFlags covers ward#1045: each --override-*
// spelling forwards as typed, and neither flag rides in uninvited.
func TestBrokerEngineerArgvForwardsOverrideFlags(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42}

	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42", "--harness", "claude",
		"--override-reservation", "--override-capacity",
	})
	got := brokerEngineerArgv(cmd, modeClaude, ref)
	for _, want := range []string{"--override-reservation", "--override-capacity"} {
		if !containsArg(got, want) {
			t.Errorf("forwarded argv missing %q: %v", want, got)
		}
	}
	bare := brokerEngineerArgv(parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42", "--harness", "claude",
	}), modeClaude, ref)
	for _, unwanted := range []string{"--override-reservation", "--override-capacity"} {
		if containsArg(bare, unwanted) {
			t.Errorf("bare forwarded argv must not carry %q: %v", unwanted, bare)
		}
	}
}

// TestValidateDispatchBrokerArgvApprovesOverrideFlags covers ward#1045: the host
// broker accepts both --override-* spellings.
func TestValidateDispatchBrokerArgvApprovesOverrideFlags(t *testing.T) {
	if err := validateDispatchBrokerArgv("engineer", []string{"--override-reservation", "--override-capacity"}); err != nil {
		t.Fatalf("validateDispatchBrokerArgv override flags: %v", err)
	}
}

func TestBrokerEngineerArgvDefaultsToCurrentReleasedWardVersion(t *testing.T) {
	origVersion := Version
	Version = "v0.569.0"
	t.Cleanup(func() { Version = origVersion })

	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42",
		"--harness", "claude",
	})
	got := brokerEngineerArgv(cmd, modeClaude, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42})
	if !argFollowedBy(got, "--ward-version", "v0.569.0") {
		t.Fatalf("brokered argv = %v, want the current released ward version to be forwarded", got)
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
	var accepted int32
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		atomic.AddInt32(&accepted, 1)
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-123")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	t.Setenv(envAgentImage, "")
	t.Setenv(envAgentTag, "")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#378", "--harness", "claude", "--workflow", "merge-remote-main",
		"--details", "repair after PR #357", "--skip-preflight", "--skip-review", "--override-capacity",
	})
	forwarded, err := (&Runner{}).maybeForwardAgentDispatchToHostBroker(t.Context(), cmd, "engineer", modeClaude)
	if err != nil {
		t.Fatalf("forward dispatch: %v", err)
	}
	if !forwarded {
		t.Fatal("dispatch did not forward despite broker env")
	}
	req := <-gotReq
	if req.Role != "engineer" || req.Requester != "director-codex-host" {
		t.Fatalf("request identity = role %q requester %q", req.Role, req.Requester)
	}
	if req.Token != "nonce-123" {
		t.Errorf("forwarded token = %q, want the per-launch nonce", req.Token)
	}
	want := []string{"engineer", forgejoBaseURL + "/coilyco-flight-deck/ward/issues/378", "--harness", "claude", "--workflow", "merge-remote-main", "--details", "repair after PR #357", "--override-capacity", "--skip-preflight", "--skip-review"}
	if !reflect.DeepEqual(req.Argv, want) {
		t.Errorf("forwarded argv = %v, want %v", req.Argv, want)
	}
	if got := atomic.LoadInt32(&accepted); got != 1 {
		t.Fatalf("broker accepted %d connections, want 1", got)
	}
}

func TestForwardAgentDispatchToHostBrokerReportsUnreachableBroker(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	t.Setenv(envDispatchBrokerAddr, addr)
	t.Setenv(envDispatchBrokerToken, "nonce-123")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	r := &Runner{Runner: &shell.Runner{
		Resolve: func(bin string) (string, error) {
			if bin == "docker" {
				t.Fatal("local launch path was attempted after broker reachability failed")
			}
			return "/bin/true", nil
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}}
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#378", "--harness", "claude", "--skip-preflight", "--skip-review",
	})
	err = r.runAgentEngineer(t.Context(), cmd, modeClaude)
	if err == nil {
		t.Fatal("runAgentEngineer unexpectedly succeeded with an unreachable broker")
	}
	var diag *dispatchBrokerDiagnostic
	if !errors.As(err, &diag) {
		t.Fatalf("runAgentEngineer returned %v, want a broker diagnostic", err)
	}
	if diag.kind != dispatchBrokerDiagnosticBrokerUnreachable {
		t.Fatalf("diagnostic kind = %q, want broker-unreachable", diag.kind)
	}
	for _, want := range []string{
		"address source: WARD_DISPATCH_BROKER_ADDR=" + addr,
		"connection: dial tcp",
		"remediation: retry after the broker service restarts; if it stays unavailable, exit this director surface and start a fresh `warded director ...` stack",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("diagnostic %q missing %q", err, want)
		}
	}
}

func TestDispatchBrokerForwardedLineIncludesLogPathWhenAvailable(t *testing.T) {
	got := dispatchBrokerForwardedLine([]string{"engineer", "coilyco-flight-deck/ward#378", "--harness", "codex", "--ward-version", "v0.569.0"}, "/tmp/ward/dispatch.log")
	for _, want := range []string{
		"ward dispatch broker: accepted `ward agent engineer coilyco-flight-deck/ward#378 --harness codex --ward-version v0.569.0`; broker Ward launch started",
		"(effective ward v0.569.0)",
		"(container visibility and engineer harness startup are pending)",
		"(broker artifact /tmp/ward/dispatch.log; inspect with `ward agent logs coilyco-flight-deck/ward#378`)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("forwarded line = %q, want %q", got, want)
		}
	}
}

func TestDispatchBrokerForwardedLineFallsBackToLookupCommandWhenPathMissing(t *testing.T) {
	got := dispatchBrokerForwardedLine([]string{"engineer", "coilyco-flight-deck/ward#902", "--harness", "codex", "--ward-version", "v0.569.0"}, "")
	for _, want := range []string{
		"ward dispatch broker: accepted `ward agent engineer coilyco-flight-deck/ward#902 --harness codex --ward-version v0.569.0`; broker Ward launch started",
		"(effective ward v0.569.0)",
		"(container visibility and engineer harness startup are pending)",
		"dispatch log path unavailable yet",
		"`ward agent logs coilyco-flight-deck/ward#902`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("forwarded line = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "forwarded `ward agent engineer") {
		t.Fatalf("forwarded line unexpectedly retained the old ambiguous success shape: %q", got)
	}
}

func TestForwardAgentDispatchToHostBrokerInheritsSurfaceHarness(t *testing.T) {
	// A Codex director surface should forward Codex engineers by default, not fall
	// back to Claude just because `warded` itself defaults that way.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-456")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	t.Setenv(envAgentImage, "")
	t.Setenv(envAgentTag, "")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#378", "--harness", "codex", "--skip-preflight", "--skip-review",
	})
	forwarded, err := (&Runner{}).maybeForwardAgentDispatchToHostBroker(t.Context(), cmd, "engineer", modeCodex)
	if err != nil {
		t.Fatalf("forward dispatch: %v", err)
	}
	if !forwarded {
		t.Fatal("codex surface dispatch did not forward despite broker env")
	}
	req := <-gotReq
	want := []string{"engineer", forgejoBaseURL + "/coilyco-flight-deck/ward/issues/378", "--harness", "codex", "--skip-preflight", "--skip-review"}
	if !reflect.DeepEqual(req.Argv, want) {
		t.Errorf("codex surface forwarded argv = %v, want %v", req.Argv, want)
	}
}

func TestForwardAgentDispatchToHostBrokerInheritsRunningDirectorHarness(t *testing.T) {
	// The broker should inherit the director's current harness from WARD_AGENT/WARD_MODE
	// when the surfaced command did not explicitly override it.
	origVersion := Version
	Version = "v0.569.0"
	t.Cleanup(func() { Version = origVersion })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-789")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	t.Setenv("WARD_AGENT", "codex")
	t.Setenv("WARD_MODE", "codex")
	t.Setenv(envAgentImage, "")
	t.Setenv(envAgentTag, "")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#378", "--skip-preflight", "--skip-review",
	})
	forwarded, err := (&Runner{}).maybeForwardAgentDispatchToHostBroker(t.Context(), cmd, "engineer", modeClaude)
	if err != nil {
		t.Fatalf("forward dispatch: %v", err)
	}
	if !forwarded {
		t.Fatal("codex director dispatch did not forward despite broker env")
	}
	req := <-gotReq
	want := []string{"engineer", forgejoBaseURL + "/coilyco-flight-deck/ward/issues/378", "--harness", "codex", "--ward-version", "v0.569.0", "--skip-preflight", "--skip-review"}
	if !reflect.DeepEqual(req.Argv, want) {
		t.Errorf("inherited-harness forwarded argv = %v, want %v", req.Argv, want)
	}
}

func TestPRWorkflowForwardedReportsUnreachableBroker(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	t.Setenv(envDispatchBrokerAddr, addr)
	t.Setenv(envDispatchBrokerToken, "nonce-123")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	handled, err := prWorkflowForwarded(t.Context(), &Runner{}, dispatchBrokerRequest{
		Action: dispatchActionPRStatus,
		Target: "coilyco-flight-deck/ward#7",
	})
	if err != nil {
		var diag *dispatchBrokerDiagnostic
		if !errors.As(err, &diag) {
			t.Fatalf("prWorkflowForwarded returned %v, want a broker diagnostic", err)
		}
		if diag.kind != dispatchBrokerDiagnosticBrokerUnreachable {
			t.Fatalf("diagnostic kind = %q, want broker-unreachable", diag.kind)
		}
		if !strings.Contains(err.Error(), "address source: WARD_DISPATCH_BROKER_ADDR="+addr) {
			t.Fatalf("diagnostic %q does not name the broker addr source", err)
		}
		return
	}
	if handled {
		t.Fatal("PR workflow forwarded despite an unreachable broker")
	}
	t.Fatal("PR workflow unexpectedly fell through with an unreachable broker")
}

func TestRunAgentListReportsUnreachableBrokerAndAvoidsLocalFallback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	t.Setenv(envDispatchBrokerAddr, addr)
	t.Setenv("WARD_READONLY", "1")
	r := &Runner{Runner: &shell.Runner{
		Resolve: func(bin string) (string, error) {
			if bin == "docker" {
				t.Fatal("local list fallback was attempted after broker reachability failed")
			}
			return "/bin/true", nil
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}}
	cmd := parseCommandForTest(t, nil, []string{"list"})
	err = r.runAgentList(t.Context(), cmd)
	if err == nil {
		t.Fatal("runAgentList unexpectedly succeeded with an unreachable broker")
	}
	var diag *dispatchBrokerDiagnostic
	if !errors.As(err, &diag) {
		t.Fatalf("runAgentList returned %v, want a broker diagnostic", err)
	}
	if diag.kind != dispatchBrokerDiagnosticBrokerUnreachable {
		t.Fatalf("diagnostic kind = %q, want broker-unreachable", diag.kind)
	}
	for _, want := range []string{
		"address source: WARD_DISPATCH_BROKER_ADDR=" + addr,
		"connection: dial tcp",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("diagnostic %q missing %q", err, want)
		}
	}
}

func TestProbeHostDispatchBrokerReportsTimeout(t *testing.T) {
	origDial := dispatchBrokerDialContext
	t.Cleanup(func() { dispatchBrokerDialContext = origDial })
	dispatchBrokerDialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	}

	err := probeHostDispatchBroker(t.Context(), "host.docker.internal:54321")
	if err == nil {
		t.Fatal("probeHostDispatchBroker unexpectedly succeeded")
	}
	var diag *dispatchBrokerDiagnostic
	if !errors.As(err, &diag) {
		t.Fatalf("probeHostDispatchBroker returned %v, want a broker diagnostic", err)
	}
	if diag.kind != dispatchBrokerDiagnosticBrokerTimeout {
		t.Fatalf("diagnostic kind = %q, want broker-timeout", diag.kind)
	}
	for _, want := range []string{
		"ward dispatch broker: broker-timeout",
		"address source: WARD_DISPATCH_BROKER_ADDR=host.docker.internal:54321",
		"connection: timed out dialing broker after 250ms",
		"remediation: retry after the broker service restarts; if it stays unavailable, exit this director surface and start a fresh `warded director ...` stack",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("timeout diagnostic %q missing %q", err, want)
		}
	}
}

func TestPRWorkflowForwardedUsesOneBrokerRequest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	var accepted int32
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		atomic.AddInt32(&accepted, 1)
		_ = req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-123")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	handled, err := prWorkflowForwarded(t.Context(), &Runner{Runner: &shell.Runner{Stdout: io.Discard}}, dispatchBrokerRequest{
		Action: dispatchActionPRStatus,
		Target: "coilyco-flight-deck/ward#7",
	})
	if err != nil {
		t.Fatalf("prWorkflowForwarded: %v", err)
	}
	if !handled {
		t.Fatal("PR workflow did not forward despite broker env")
	}
	if got := atomic.LoadInt32(&accepted); got != 1 {
		t.Fatalf("broker accepted %d connections, want 1", got)
	}
}

func TestForwardAgentDispatchToHostBrokerSupportsQa(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-qa")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	cmd := parseCommandForTest(t, agentQAFlags(), []string{
		"qa", "coilyco-flight-deck/ward#378", "--harness", "claude", "inspect the branch",
	})
	forwarded, err := (&Runner{}).maybeForwardAgentDispatchToHostBroker(t.Context(), cmd, "qa", modeClaude)
	if err != nil {
		t.Fatalf("forward QA dispatch: %v", err)
	}
	if !forwarded {
		t.Fatal("qa dispatch did not forward despite broker env")
	}
	req := <-gotReq
	want := []string{"qa", "coilyco-flight-deck/ward#378", "--harness", "claude", "--family", "internal", "--thoroughness", "standard", "inspect the branch"}
	if !reflect.DeepEqual(req.Argv, want) {
		t.Errorf("qa forwarded argv = %v, want %v", req.Argv, want)
	}
}

// TestSendDispatchBrokerLaunchRequestWaitsForResponse pins the durability fix:
// a launch must not read as successful until the broker actually answers.
func TestSendDispatchBrokerLaunchRequestWaitsForResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	release := make(chan struct{})
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		<-release
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, LogPath: "/tmp/ward/dispatch.log"})
	})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan struct {
		logPath string
		err     error
	}, 1)
	go func() {
		logPath, err := sendDispatchBrokerLaunchRequest(ctx, ln.Addr().String(), dispatchBrokerRequest{
			Role:      "qa",
			Argv:      []string{"qa", "coilyco-flight-deck/ward#378", "--harness", "codex"},
			Requester: "director-codex-host",
			Token:     "nonce-adv",
		})
		done <- struct {
			logPath string
			err     error
		}{logPath: logPath, err: err}
	}()

	select {
	case <-gotReq:
	case <-time.After(2 * time.Second):
		t.Fatal("broker never received the launch request")
	}
	select {
	case got := <-done:
		t.Fatalf("launch returned before the broker responded: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("launch request: %v", got.err)
		}
		if got.logPath != "/tmp/ward/dispatch.log" {
			t.Fatalf("log path = %q, want the broker response", got.logPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("launch never returned after the broker responded")
	}
}

// TestSendDispatchBrokerLaunchRequestRetriesDroppedResponse proves an automatic
// retry reuses the original request id, so the broker can return one accepted run.
func TestSendDispatchBrokerLaunchRequestRetriesDroppedResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	requestIDs := make(chan string, 2)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		func() {
			defer conn.Close()
			var req dispatchBrokerRequest
			if err := json.NewDecoder(conn).Decode(&req); err != nil {
				return
			}
			requestIDs <- req.RequestID
		}()
		conn, err = ln.Accept()
		if err != nil {
			return
		}
		func() {
			defer conn.Close()
			var req dispatchBrokerRequest
			if err := json.NewDecoder(conn).Decode(&req); err != nil {
				return
			}
			requestIDs <- req.RequestID
			_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, LogPath: "/tmp/ward/dispatch.log"})
		}()
	}()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	logPath, err := sendDispatchBrokerLaunchRequest(ctx, ln.Addr().String(), dispatchBrokerRequest{
		Role:      "engineer",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#378", "--harness", "codex"},
		Requester: "director-codex-host",
		Token:     "nonce-eof",
	})
	if err != nil {
		t.Fatalf("retry launch request: %v", err)
	}
	if logPath != "/tmp/ward/dispatch.log" {
		t.Fatalf("log path = %q, want the accepted broker response", logPath)
	}
	first, second := <-requestIDs, <-requestIDs
	if first == "" || first != second {
		t.Fatalf("retry request ids = %q and %q, want one stable non-empty id", first, second)
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

func TestDispatchBrokerPanicErrorClassifiesExit125NameConflictAsRequestFailure(t *testing.T) {
	err := dispatchBrokerPanicError("launch worker", errors.New(`exit status 125: Conflict. The container name "/engineer-codex-ward-786" is already in use`))
	if !strings.Contains(err.Error(), "request failure") {
		t.Fatalf("panic error = %q, want a request failure classification", err)
	}
	if !strings.Contains(err.Error(), "exit status 125") {
		t.Fatalf("panic error = %q, want the exit-125 signal", err)
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("panic error = %q, want the underlying Docker conflict", err)
	}
}

func TestDispatchPartialLaunchErrorClassifiesAsPartialLaunch(t *testing.T) {
	err := newDispatchPartialLaunchError(
		agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1360},
		"engineer-codex-ward-1360",
		errors.New("reservation comment failed"),
	)
	if isEngineerCapacityError(err) {
		t.Fatal("partial-launch error must not classify as engineer capacity backpressure")
	}
	if got := dispatchArtifactOutcome(err); got != "partial-launch" {
		t.Fatalf("artifact outcome = %q, want partial-launch", got)
	}
	if got := dispatchArtifactErrorClass(err); got != "partial-launch" {
		t.Fatalf("artifact error class = %q, want partial-launch", got)
	}
	for _, want := range []string{
		"partial-launch for coilyco-flight-deck/ward#1360",
		"engineer-codex-ward-1360",
		"reservation comment failed",
		"re-post the reservation comment or stop and re-dispatch engineer-codex-ward-1360",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("partial-launch error missing %q:\n%s", want, err.Error())
		}
	}
}

// TestServeHostDispatchBrokerSurvivesExit125NameConflict keeps the listener alive
// after a brokered launch hits Docker's duplicate-name exit-125 refusal.
func TestServeHostDispatchBrokerSurvivesExit125NameConflict(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("FORGEJO_TOKEN", "broker-token")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	origLaunch := dispatchBrokerLaunch
	origFailedHook := dispatchFailedDispatchLaunchHook
	origRestoreHook := dispatchStdioRestoreHook
	failed := make(chan struct{})
	restored := make(chan struct{})
	t.Cleanup(func() {
		dispatchBrokerLaunch = origLaunch
		dispatchFailedDispatchLaunchHook = origFailedHook
		dispatchStdioRestoreHook = origRestoreHook
		_ = ln.Close()
	})

	var launches atomic.Int32
	dispatchFailedDispatchLaunchHook = func(dispatchBrokerRequest, string, error) bool {
		close(failed)
		return true
	}
	dispatchStdioRestoreHook = func() { close(restored) }
	dispatchBrokerLaunch = func(context.Context, dispatchBrokerRequest) error {
		if launches.Add(1) == 1 {
			panic(errors.New(`exit status 125: Conflict. The container name "/engineer-codex-ward-786" is already in use`))
		}
		return nil
	}

	go (&Runner{}).serveHostDispatchBroker(ctx, ln, "director-box", "secret")

	first := roundTripDispatchBrokerRequest(t, ln.Addr().String(), dispatchBrokerRequest{
		Role:  "engineer",
		Argv:  []string{"engineer", "coilyco-flight-deck/ward#786", "--harness", "codex", "--pr"},
		Token: "secret",
	})
	if !first.OK {
		t.Fatalf("accepted launch response = %+v, want successful detach", first)
	}
	if first.LogPath == "" {
		t.Fatal("accepted launch response omitted the durable dispatch artifact path")
	}
	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("asynchronous duplicate-name failure never reached broker recovery")
	}
	select {
	case <-restored:
	case <-time.After(2 * time.Second):
		t.Fatal("asynchronous duplicate-name failure never finalized the launch worker")
	}

	second := roundTripDispatchBrokerRequest(t, ln.Addr().String(), dispatchBrokerRequest{
		Role:  "nope",
		Argv:  []string{"nope"},
		Token: "secret",
	})
	if second.OK {
		t.Fatal("validation failure unexpectedly returned OK")
	}
	if !strings.Contains(second.Error, "refused") {
		t.Fatalf("second response = %q, want a live broker refusal", second.Error)
	}
}

// Broker env stays clear while the asynchronously-owned host launch continues.
func TestRunHostDispatchBrokerRequestDetachesAfterHostLaunchStarts(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Cleanup(func() {
		// Let the temp-home teardown own the whole .ward tree; per-artifact
		// removal races the broker's final redacted/raw artifact writes.
		_ = os.RemoveAll(filepath.Join(home, ".ward"))
	})
	origReadOnly, hadReadOnly := os.LookupEnv("WARD_READONLY")
	origAddr, hadAddr := os.LookupEnv(envDispatchBrokerAddr)
	origToken, hadToken := os.LookupEnv(envDispatchBrokerToken)
	if err := os.Setenv("WARD_READONLY", "1"); err != nil {
		t.Fatalf("set WARD_READONLY: %v", err)
	}
	if err := os.Setenv(envDispatchBrokerAddr, "127.0.0.1:4321"); err != nil {
		t.Fatalf("set %s: %v", envDispatchBrokerAddr, err)
	}
	if err := os.Setenv(envDispatchBrokerToken, "broker-token"); err != nil {
		t.Fatalf("set %s: %v", envDispatchBrokerToken, err)
	}
	t.Cleanup(func() {
		if hadReadOnly {
			_ = os.Setenv("WARD_READONLY", origReadOnly)
		} else {
			_ = os.Unsetenv("WARD_READONLY")
		}
		if hadAddr {
			_ = os.Setenv(envDispatchBrokerAddr, origAddr)
		} else {
			_ = os.Unsetenv(envDispatchBrokerAddr)
		}
		if hadToken {
			_ = os.Setenv(envDispatchBrokerToken, origToken)
		} else {
			_ = os.Unsetenv(envDispatchBrokerToken)
		}
	})

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	result := make(chan struct {
		logPath string
		err     error
	}, 1)
	var logPath string
	origLaunch := dispatchBrokerLaunch
	t.Cleanup(func() { dispatchBrokerLaunch = origLaunch })
	dispatchBrokerLaunch = func(_ context.Context, _ dispatchBrokerRequest) error {
		defer close(done)
		if got := os.Getenv("WARD_READONLY"); got != "" {
			t.Errorf("host launch inherited WARD_READONLY=%q; want it cleared", got)
		}
		if got := os.Getenv(envDispatchBrokerAddr); got != "" {
			t.Errorf("host launch inherited %s=%q; want it cleared", envDispatchBrokerAddr, got)
		}
		if got := os.Getenv(envDispatchBrokerToken); got != "" {
			t.Errorf("host launch inherited %s=%q; want it cleared", envDispatchBrokerToken, got)
		}
		close(started)
		<-release
		return nil
	}

	req := dispatchBrokerRequest{
		Role: "qa",
		Argv: []string{"qa", "coilyco-flight-deck/ward#795", "--harness", "codex"},
	}
	go func() {
		gotLogPath, err := (&Runner{}).startHostDispatchBrokerRequest(t.Context(), req)
		result <- struct {
			logPath string
			err     error
		}{logPath: gotLogPath, err: err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("host launch never started")
	}
	var got struct {
		logPath string
		err     error
	}
	select {
	case got = <-result:
		if got.err != nil {
			t.Fatalf("startHostDispatchBrokerRequest: %v", got.err)
		}
		logPath = got.logPath
		if !strings.Contains(got.logPath, "dispatch") {
			t.Fatalf("log path %q does not look like a dispatch log", got.logPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startHostDispatchBrokerRequest did not detach after host launch started")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("host launch never finished")
	}
	body, err := os.ReadFile(logPath) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read dispatch log: %v", err)
	}
	logText := string(body)
	for _, want := range []string{
		"ward dispatch broker: this log captures the broker wrapper only",
		"ward agent logs coilyco-flight-deck/ward#795",
		"broker accepted launch request",
		"broker Ward launch started; response detaches before container visibility and engineer harness start",
		"broker launch completed; container visibility was confirmed, but engineer harness startup remains in-container",
	} {
		if !strings.Contains(logText, want) {
			t.Errorf("dispatch log missing %q\n%s", want, logText)
		}
	}
	deadline := time.After(2 * time.Second)
	for os.Getenv("WARD_READONLY") != "1" || os.Getenv(envDispatchBrokerAddr) != "127.0.0.1:4321" || os.Getenv(envDispatchBrokerToken) != "broker-token" {
		select {
		case <-deadline:
			t.Fatalf("expected broker env to be restored after launch, got WARD_READONLY=%q %s=%q %s=%q",
				os.Getenv("WARD_READONLY"), envDispatchBrokerAddr, os.Getenv(envDispatchBrokerAddr), envDispatchBrokerToken, os.Getenv(envDispatchBrokerToken))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// TestRunHostDispatchBrokerRequestReportsLaterLaunchFailureThroughArtifact locks the
// detach contract: an accepted response still finalizes host failures in its artifact.
func TestRunHostDispatchBrokerRequestReportsLaterLaunchFailureThroughArtifact(t *testing.T) {
	home, err := os.MkdirTemp("", "ward-dispatch-failure-home-*")
	if err != nil {
		t.Fatalf("temp home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	setTestHome(t, home)
	done := make(chan struct{})
	recoveryStarted := make(chan struct{})
	restored := make(chan struct{})
	origLaunch := dispatchBrokerLaunch
	origRestoreHook := dispatchStdioRestoreHook
	origFailedDispatchHook := dispatchFailedDispatchLaunchHook
	origFailedDispatchStartHook := dispatchFailedDispatchLaunchStartHook
	t.Cleanup(func() {
		dispatchBrokerLaunch = origLaunch
		defer func() { dispatchStdioRestoreHook = origRestoreHook }()
		dispatchFailedDispatchLaunchHook = origFailedDispatchHook
		dispatchFailedDispatchLaunchStartHook = origFailedDispatchStartHook
		select {
		case <-restored:
		case <-time.After(30 * time.Second):
			t.Fatal("structured launch failure never restored stdio")
		}
	})
	dispatchStdioRestoreHook = func() {
		select {
		case <-restored:
		default:
			close(restored)
		}
	}
	dispatchFailedDispatchLaunchHook = func(dispatchBrokerRequest, string, error) bool { return true }
	dispatchFailedDispatchLaunchStartHook = func() {
		select {
		case <-recoveryStarted:
		default:
			close(recoveryStarted)
		}
	}
	dispatchBrokerLaunch = func(context.Context, dispatchBrokerRequest) error {
		defer close(done)
		return errors.New(`Conflict. The container name "/engineer-codex-ward-786" is already in use`)
	}
	r := &Runner{Runner: &shell.Runner{Resolve: func(bin string) (string, error) {
		if bin == "ward" {
			return "/bin/true", nil
		}
		return "", fmt.Errorf("unexpected binary %q", bin)
	}}}
	req := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", "coilyco-flight-deck/ward#786", "--harness", "codex", "--pr"},
	}
	logPath, err := r.startHostDispatchBrokerRequest(t.Context(), req)
	if err != nil {
		t.Fatalf("startHostDispatchBrokerRequest: %v", err)
	}
	if !strings.Contains(logPath, "dispatch") {
		t.Fatalf("log path %q does not look like a dispatch log", logPath)
	}
	<-done
	<-recoveryStarted
	<-restored
	summaryPath := filepath.Join(filepath.Dir(logPath), dispatchArtifactSummaryFile) // #nosec G304 -- test-owned artifact path
	var summary []byte
	deadline := time.After(2 * time.Second)
	for {
		var readErr error
		summary, readErr = os.ReadFile(summaryPath) // #nosec G304 -- test-owned artifact path
		if readErr == nil && strings.Contains(string(summary), "outcome: failed-before-container") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("dispatch summary did not reach its terminal failure state:\n%s", summary)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	for _, want := range []string{"outcome: failed-before-container", "already in use"} {
		if !strings.Contains(string(summary), want) {
			t.Fatalf("dispatch summary missing %q:\n%s", want, summary)
		}
	}
}

// TestCommentFailedDispatch writes the failure comment that supersedes a stale
// reservation when the forwarded launch never becomes a running engineer.
func TestCommentFailedDispatch(t *testing.T) {
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 689}
	f := &fakeLockForge{
		listComments: []issueComment{
			{ID: 11, Body: reservationCommentBody(modeCodex, "engineer-codex-ward-689", "host", time.Now().Add(-time.Minute), "", nil), CreatedAt: time.Now().Add(-time.Minute)},
		},
	}
	req := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", ref.String(), "--harness", "codex", "--skip-preflight"},
	}

	r.commentFailedDispatch(context.Background(), f, modeCodex, ref, req, "/tmp/ward/dispatch.log", errors.New("exit status 1"))

	if f.unlocked != 1 {
		t.Fatalf("unlockIssue called %d times, want 1", f.unlocked)
	}
	if len(f.comments) != 0 {
		t.Fatalf("commentIssue called %d times, want 0", len(f.comments))
	}
	if got := fmt.Sprintf("%v", f.deleted); got != "[11]" {
		t.Fatalf("deleted comments = %s, want [11]", got)
	}
}

// TestCommentFailedDispatchSkipsStopOnDockerNameConflict keeps the live container intact.
// Docker's duplicate-name refusal must not stop the existing run.
func TestCommentFailedDispatchSkipsStopOnDockerNameConflict(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(testShellPath(logPath)) + "\n" +
		"exit 0\n"
	writeTestShellCommand(t, script, body)

	r := &Runner{Runner: &shell.Runner{
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Resolve: func(string) (string, error) { return script, nil },
	}}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 689}
	f := &fakeLockForge{
		listComments: []issueComment{
			{ID: 11, Body: reservationCommentBody(modeCodex, "engineer-codex-ward-689", "host", time.Now().Add(-time.Minute), "", nil), CreatedAt: time.Now().Add(-time.Minute)},
		},
	}
	req := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", ref.String(), "--harness", "codex", "--skip-preflight"},
	}

	r.commentFailedDispatch(context.Background(), f, modeCodex, ref, req, "/tmp/ward/dispatch.log", errors.New(`Conflict. The container name "/engineer-codex-ward-689" is already in use`))

	if f.unlocked != 1 {
		t.Fatalf("unlockIssue called %d times, want 1", f.unlocked)
	}
	if len(f.comments) != 0 {
		t.Fatalf("commentIssue called %d times, want 0", len(f.comments))
	}
	if got := fmt.Sprintf("%v", f.deleted); got != "[11]" {
		t.Fatalf("deleted comments = %s, want [11]", got)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("docker stop was invoked for the conflict case, log = %q", readFile(t, logPath))
	}
}

// TestCommentReservationConflictDispatch pins ward#1149: a collision defers without
// touching the live hold (no release marker, no unlock) and leaves the sweep signal.
func TestCommentReservationConflictDispatch(t *testing.T) {
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 927}
	f := &fakeLockForge{}
	req := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", ref.String(), "--harness", "codex"},
	}
	conflict := newReservationConflict(
		"ward agent engineer --harness codex: issue %s is already reserved remotely (by @coilyco-ops); wait for it to finish or pass --override-reservation to override", ref)

	r.commentReservationConflictDispatch(context.Background(), f, modeCodex, ref, req, "/tmp/ward/dispatch.log", conflict)

	if f.unlocked != 0 {
		t.Fatalf("unlockIssue called %d times, want 0 - the live run's seal must stay", f.unlocked)
	}
	if len(f.comments) != 1 {
		t.Fatalf("commentIssue called %d times, want 1", len(f.comments))
	}
	body := f.comments[0]
	if strings.Contains(body, agentReservationReleaseMarker) {
		t.Errorf("conflict comment must not carry the release marker (the hold belongs to the live run)\n%s", body)
	}
	for _, want := range []string{
		agentNeedsRedispatchMarker,
		"WARD-WORKFLOW: dispatch-deferred",
		"reservation-collision details",
		"Attempted harness: `codex`",
		"Attempted run: `ward agent engineer coilyco-flight-deck/ward#927 --harness codex`",
		"Host log: `/tmp/ward/dispatch.log`",
		"terminal `WARD-WORKFLOW` outcome supersedes its reservation (ward#1149)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("conflict comment missing %q\n%s", want, body)
		}
	}
}

// TestCommentDeferredDispatch writes the backpressure comment that supersedes a
// stale reservation when the forwarded launch hits the global engineer cap.
func TestCommentDeferredDispatch(t *testing.T) {
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 902}
	f := &fakeLockForge{
		listComments: []issueComment{
			{ID: 21, Body: dispatchLaunchDeferredCommentBody(modeCodex, "engineer-codex-ward-902", dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", ref.String(), "--harness", "codex", "--skip-preflight"}}, "/tmp/ward/dispatch.log", newEngineerCapacityError("ward agent engineer --harness codex", 10, 10)), CreatedAt: time.Now().Add(-time.Minute)},
		},
	}
	req := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", ref.String(), "--harness", "codex", "--skip-preflight"},
	}
	capacityErr := newEngineerCapacityError("ward agent engineer --harness codex", 10, 10)

	r.commentDeferredDispatch(context.Background(), f, modeCodex, ref, req, "/tmp/ward/dispatch.log", capacityErr)

	if f.unlocked != 1 {
		t.Fatalf("unlockIssue called %d times, want 1", f.unlocked)
	}
	if len(f.comments) != 0 {
		t.Fatalf("commentIssue called %d times, want 0", len(f.comments))
	}
	if got := fmt.Sprintf("%v", f.deleted); got != "[21]" {
		t.Fatalf("deleted comments = %s, want [21]", got)
	}
}

// TestCommentDispatchLaunchErrorReportsCapacityLocally keeps pool-full backpressure
// on stderr only so the issue thread stays untouched.
func TestCommentDispatchLaunchErrorReportsCapacityLocally(t *testing.T) {
	r := &Runner{}
	req := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", "coilyco-flight-deck/ward#902", "--harness", "codex"},
	}
	capacityErr := newEngineerCapacityError("ward agent engineer --harness codex", 10, 10)

	stderr := captureTestStderr(t, func() {
		r.commentDispatchLaunchError(context.Background(), req, "/tmp/ward/dispatch.log", capacityErr)
	})
	for _, want := range []string{
		"ward dispatch broker: capacity-defer: engineer pool full, 10/10 active launches, not dispatched",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("capacity stderr missing %q: %q", want, stderr)
		}
	}
}

func TestStopFailedDispatchContainerStopsTheAttemptedEngineer(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	name := issueScopedContainerName(roleEngineer, modeCodex, targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}, 689)
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(testShellPath(logPath)) + "\n" +
		"case \"$1\" in\n" +
		"  ps) printf '%s\\n' '" + name + "' ;;\n" +
		"  stop) exit 0 ;;\n" +
		"esac\n" +
		"exit 0\n"
	writeTestShellCommand(t, script, body)
	r := &Runner{Runner: &shell.Runner{
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Resolve: func(string) (string, error) { return script, nil },
	}}

	r.stopFailedDispatchContainer(context.Background(), modeCodex, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 689}, roleEngineer, name)

	log := readFile(t, logPath)
	if !strings.Contains(log, "ps --filter name=^"+name+"$ --format {{.Names}}") {
		t.Fatalf("stop helper did not probe the running container:\n%s", log)
	}
	if !strings.Contains(log, "stop "+name) {
		t.Fatalf("stop helper did not stop the attempted container:\n%s", log)
	}
}

// TestCommentDeferredReleaseAssetsDispatch writes the backpressure comment that
// supersedes a stale reservation when the selected release lacks its asset.
func TestCommentDeferredReleaseAssetsDispatch(t *testing.T) {
	r := &Runner{}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 903}
	f := &fakeLockForge{
		listComments: []issueComment{
			{ID: 31, Body: dispatchLaunchReleaseAssetsDeferredCommentBody(modeCodex, "engineer-codex-ward-903", dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", ref.String(), "--harness", "codex", "--skip-preflight"}}, "/tmp/ward/dispatch.log", newReleaseAssetsNotReadyError("v0.544.0", "ward-linux-arm64", "Not Found")), CreatedAt: time.Now().Add(-time.Minute)},
		},
	}
	req := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", ref.String(), "--harness", "codex", "--skip-preflight"},
	}
	assetErr := newReleaseAssetsNotReadyError("v0.544.0", "ward-linux-arm64", "Not Found")

	r.commentDeferredReleaseAssetsDispatch(context.Background(), f, modeCodex, ref, req, "/tmp/ward/dispatch.log", assetErr)

	if f.unlocked != 1 {
		t.Fatalf("unlockIssue called %d times, want 1", f.unlocked)
	}
	if len(f.comments) != 0 {
		t.Fatalf("commentIssue called %d times, want 0", len(f.comments))
	}
	if got := fmt.Sprintf("%v", f.deleted); got != "[31]" {
		t.Fatalf("deleted comments = %s, want [31]", got)
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

// TestRunAgentTaskDirectRoutesThroughBrokerOnReadonlySurface is the ward#931 smoke.
// It locks the ward#900 and ward#876 regression shape without a live LLM.
func TestRunAgentTaskDirectRoutesThroughBrokerOnReadonlySurface(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("FORGEJO_TOKEN", "forgejo-token")
	t.Setenv("WARD_AGENT", "codex")
	t.Setenv("WARD_MODE", "codex")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv(envDispatchBrokerToken, "nonce-freeform")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")

	bundleDir := t.TempDir()
	defaultsBody := canonicalSmartDefaultsBlock(t, func(defs *smartDefaults) {
		defs.directorMaxParallel = 10
	}) + `
workflow default=merge-remote-main {
}
`
	reposBody := `repos {
    repo-authority default=forgejo {
        trusted-owner coilysiren
        trusted-owner coilyco-flight-deck
        repo "coilysiren/*" forge=github
    }
}`
	if err := os.WriteFile(filepath.Join(bundleDir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write bundle defaults: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, bundleFixtureReposPath), []byte(reposBody), 0o644); err != nil {
		t.Fatalf("write bundle repos: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+bundleDir)

	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("load trusted bundle: %v", err)
	}
	if !slices.Contains(defs.trustedOwners, "coilyco-flight-deck") {
		t.Fatalf("trusted owners = %v, want coilyco-flight-deck in the real bundle", defs.trustedOwners)
	}

	issueCreated := make(chan struct{}, 1)
	issueErr := make(chan error, 1)
	var issueReq struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	forgejo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/coilyco-flight-deck/agentic-os/issues":
			if got := r.Header.Get("Authorization"); got != "token forgejo-token" {
				select {
				case issueErr <- fmt.Errorf("authorization header = %q, want token forgejo-token", got):
				default:
				}
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&issueReq); err != nil {
				select {
				case issueErr <- fmt.Errorf("decode issue body: %w", err):
				default:
				}
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]int{"number": 400})
			select {
			case issueCreated <- struct{}{}:
			default:
			}
		default:
			select {
			case issueErr <- fmt.Errorf("unexpected Forgejo request: %s %s", r.Method, r.URL.Path):
			default:
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer forgejo.Close()

	origForgejoBase := forgejoBaseURL
	forgejoBaseURL = forgejo.URL
	t.Cleanup(func() { forgejoBaseURL = origForgejoBase })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()

	gotReq := make(chan dispatchBrokerRequest, 1)
	brokerErr := make(chan error, 1)
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, req dispatchBrokerRequest) {
		gotReq <- req
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true, LogPath: "/tmp/ward/dispatch.log"})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv("WARD_FORGEJO_BASE", forgejo.URL)
	t.Setenv(envAgentImage, "")
	t.Setenv(envAgentTag, "")

	dockerName := issueScopedContainerName(roleEngineer, modeCodex, targetRepo{Owner: "coilyco-flight-deck", Name: "agentic-os"}, 400)
	dockerScript := filepath.Join(t.TempDir(), "docker")
	writeTestShellCommand(t, dockerScript, "#!/bin/sh\n"+
		"if [ \"$1\" = ps ]; then\n"+
		"  printf '%s\\n' "+shellQuote(dockerName)+"\n"+
		"  exit 0\n"+
		"fi\n"+
		"exit 0\n")
	r := &Runner{Runner: &shell.Runner{
		Resolve: func(bin string) (string, error) {
			if bin == "docker" {
				return dockerScript, nil
			}
			return "/bin/true", nil
		},
	}}
	instructions := filepath.Join(t.TempDir(), "engineer-dispatch-smoke.md")
	if err := os.WriteFile(instructions, []byte("Repair the launch path so read-only engineer dispatch uses the broker."), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/agentic-os", "--instructions-file", instructions, "--skip-preflight",
	})
	if err := r.runAgentTask(context.Background(), cmd, modeCodex); err != nil {
		t.Fatalf("runAgentTaskDirect smoke: %v", err)
	}

	select {
	case <-issueCreated:
	case <-time.After(2 * time.Second):
		t.Fatal("freeform engineer never filed the issue")
	}
	select {
	case err := <-issueErr:
		t.Fatalf("forgejo smoke server: %v", err)
	default:
	}
	if got, want := issueReq.Title, "Repair the launch path so read-only engineer dispatch uses the broker."; got != want {
		t.Fatalf("filed issue title = %q, want %q", got, want)
	}
	if !strings.Contains(issueReq.Body, "Filed by `ward agent engineer --harness codex`.") {
		t.Fatalf("filed issue body did not carry the provenance footer:\n%s", issueReq.Body)
	}

	var req dispatchBrokerRequest
	select {
	case req = <-gotReq:
	case err := <-brokerErr:
		t.Fatalf("broker smoke server: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("engineer launch never reached the broker")
	}
	if req.Role != "engineer" {
		t.Fatalf("broker role = %q, want engineer", req.Role)
	}
	wantArgv := []string{"engineer", forgejoBaseURL + "/coilyco-flight-deck/agentic-os/issues/400", "--harness", "codex", "--skip-preflight"}
	if !reflect.DeepEqual(req.Argv, wantArgv) {
		t.Fatalf("broker argv = %v, want %v", req.Argv, wantArgv)
	}
	if req.Requester != "director-codex-host" {
		t.Fatalf("broker requester = %q, want director-codex-host", req.Requester)
	}
	if req.Token != "nonce-freeform" {
		t.Fatalf("broker token = %q, want nonce-freeform", req.Token)
	}
}

func TestSurfaceDispatchModeInheritsRunningDirectorHarness(t *testing.T) {
	t.Setenv(envDispatchBrokerAddr, "127.0.0.1:12345")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_AGENT", "codex")
	t.Setenv("WARD_MODE", "codex")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", "coilyco-flight-deck/ward#1"})
	got, err := surfaceDispatchMode(cmd)
	if err != nil {
		t.Fatalf("surfaceDispatchMode: %v", err)
	}
	if got != modeCodex {
		t.Errorf("surfaceDispatchMode() = %q, want codex", got)
	}
}

func TestSurfaceDispatchModeExplicitOverrideWins(t *testing.T) {
	t.Setenv(envDispatchBrokerAddr, "127.0.0.1:12345")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_AGENT", "codex")
	t.Setenv("WARD_MODE", "codex")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", "coilyco-flight-deck/ward#1", "--harness", "claude"})
	got, err := surfaceDispatchMode(cmd)
	if err != nil {
		t.Fatalf("surfaceDispatchMode: %v", err)
	}
	if got != modeClaude {
		t.Errorf("surfaceDispatchMode explicit override = %q, want claude", got)
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
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, _ dispatchBrokerRequest) {
		// Mimic the credential broker refusing the dispatch protocol handshake.
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{
			OK:    false,
			Error: "unsupported protocol version 0 (want 1)",
		})
	})

	_, err = sendDispatchBrokerRequest(t.Context(), ln.Addr().String(), dispatchBrokerRequest{Role: "engineer"})
	if err == nil {
		t.Fatal("credential-broker reply unexpectedly accepted")
	}
	if !strings.Contains(err.Error(), "wrong broker") {
		t.Errorf("error %q does not carry the wrong-broker hint", err)
	}
}

// TestForwardAgentDispatchIncludesDispatchLogOnLaunchFailure keeps the director-surface
// error structured when the broker rejects a launch.
func TestForwardAgentDispatchIncludesDispatchLogOnLaunchFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, _ dispatchBrokerRequest) {
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{
			OK:      false,
			Error:   `Conflict. The container name "/engineer-codex-ward-786" is already in use`,
			LogPath: "/tmp/ward-agent-logs/dispatch/20260709T083000Z-director-codex-ward-786.log",
		})
	})

	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-786")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-ward-786")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", "coilyco-flight-deck/ward#786"})
	forwarded, err := (&Runner{}).maybeForwardAgentDispatchToHostBroker(context.Background(), cmd, "engineer", modeCodex)
	if !forwarded {
		t.Fatal("dispatch was not forwarded through the broker")
	}
	if err == nil {
		t.Fatal("broker launch failure unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("error %q does not carry the Docker conflict", err)
	}
	if !strings.Contains(err.Error(), "/tmp/ward-agent-logs/dispatch/20260709T083000Z-director-codex-ward-786.log") {
		t.Fatalf("error %q does not carry the dispatch log path", err)
	}
}

func TestForwardAgentDispatchPrintsLookupCommandWhenLaunchSucceedsWithoutLogPath(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen broker: %v", err)
	}
	defer ln.Close()
	serveDispatchBrokerRequests(t, ln, func(conn net.Conn, _ dispatchBrokerRequest) {
		_ = json.NewEncoder(conn).Encode(dispatchBrokerResponse{OK: true})
	})

	origStderr := os.Stderr
	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	t.Cleanup(func() {
		_ = wPipe.Close()
		_ = rPipe.Close()
		os.Stderr = origStderr
	})
	os.Stderr = wPipe
	t.Setenv(envDispatchBrokerAddr, ln.Addr().String())
	t.Setenv(envDispatchBrokerToken, "nonce-902")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_CONTAINER_NAME", "director-codex-host")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", "coilyco-flight-deck/ward#902"})
	r := &Runner{}
	forwarded, ferr := r.maybeForwardAgentDispatchToHostBroker(context.Background(), cmd, "engineer", modeCodex)
	_ = wPipe.Close()
	if ferr != nil {
		t.Fatalf("forwarded launch without log path returned error: %v", ferr)
	}
	if !forwarded {
		t.Fatal("launch without log path did not forward")
	}
	var stderr bytes.Buffer
	if _, err := io.Copy(&stderr, rPipe); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	got := stderr.String()
	if !strings.Contains(got, "dispatch log path unavailable yet") {
		t.Fatalf("stderr = %q, want a clear no-path reason", got)
	}
	if !strings.Contains(got, "`ward agent logs "+forgejoBaseURL+"/coilyco-flight-deck/ward/issues/902`") {
		t.Fatalf("stderr = %q, want a deterministic lookup command", got)
	}
}

func TestStartHostDispatchBrokerRequestDetachesBeforeEngineerVisibility(t *testing.T) {
	setTestHome(t, t.TempDir())
	r := fakeEngineerVisibilityDockerRunner(t, "engineer-codex-ward-1087", 2)

	origLaunch := dispatchBrokerLaunch
	origRestoreHook := dispatchStdioRestoreHook
	t.Cleanup(func() {
		dispatchBrokerLaunch = origLaunch
		dispatchStdioRestoreHook = origRestoreHook
	})
	launchEntered := make(chan struct{})
	releaseLaunch := make(chan struct{})
	finished := make(chan struct{})
	dispatchStdioRestoreHook = func() { close(finished) }
	dispatchBrokerLaunch = func(context.Context, dispatchBrokerRequest) error {
		close(launchEntered)
		<-releaseLaunch
		return nil
	}

	req := dispatchBrokerRequest{
		Role:      "engineer",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1087", "--harness", "codex", "--pr"},
		Requester: "director-codex-host",
		Token:     "nonce-visible",
	}
	result := make(chan error, 1)
	go func() {
		_, err := r.startHostDispatchBrokerRequest(context.Background(), req)
		result <- err
	}()
	select {
	case <-launchEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("broker Ward launch never began")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("startHostDispatchBrokerRequest: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("broker did not detach before container visibility")
	}
	close(releaseLaunch)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("asynchronous host launch did not finish")
	}
}

// waitForDispatchArtifactSummary synchronizes assertions with the durable
// terminal artifact, not an earlier recovery milestone in the detached worker.
func waitForDispatchArtifactSummary(t *testing.T, summaryPath string, wants ...string) string {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		summary, err := os.ReadFile(summaryPath) // #nosec G304 -- test-owned artifact path
		if err == nil {
			body := string(summary)
			ready := true
			for _, want := range wants {
				if !strings.Contains(body, want) {
					ready = false
					break
				}
			}
			if ready {
				return body
			}
		}
		select {
		case <-deadline.C:
			if err != nil {
				t.Fatalf("read dispatch summary: %v", err)
			}
			t.Fatalf("dispatch summary did not reach terminal state %q:\n%s", wants, summary)
		case <-poll.C:
		}
	}
}

func TestStartHostDispatchBrokerRequestReportsMissingEngineerVisibilityAsynchronously(t *testing.T) {
	setTestHome(t, t.TempDir())
	r := fakeEngineerVisibilityDockerRunner(t, "", 0)

	origLaunch := dispatchBrokerLaunch
	origTimeout := dispatchBrokerVisibilityTimeout
	origPoll := dispatchBrokerVisibilityPoll
	origFailedHook := dispatchFailedDispatchLaunchHook
	origRestoreHook := dispatchStdioRestoreHook
	recoveryStarted := make(chan struct{})
	finished := make(chan struct{})
	t.Cleanup(func() {
		dispatchBrokerLaunch = origLaunch
		dispatchBrokerVisibilityTimeout = origTimeout
		dispatchBrokerVisibilityPoll = origPoll
		dispatchFailedDispatchLaunchHook = origFailedHook
		dispatchStdioRestoreHook = origRestoreHook
	})
	dispatchBrokerVisibilityTimeout = 75 * time.Millisecond
	dispatchBrokerVisibilityPoll = 10 * time.Millisecond
	dispatchFailedDispatchLaunchHook = func(dispatchBrokerRequest, string, error) bool {
		select {
		case <-recoveryStarted:
		default:
			close(recoveryStarted)
		}
		return true
	}
	dispatchStdioRestoreHook = func() {
		select {
		case <-finished:
		default:
			close(finished)
		}
	}
	dispatchBrokerLaunch = func(context.Context, dispatchBrokerRequest) error { return nil }

	req := dispatchBrokerRequest{
		Role:      "engineer",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1087", "--harness", "codex", "--pr"},
		Requester: "director-codex-host",
		Token:     "nonce-missing",
	}
	logPath, err := r.startHostDispatchBrokerRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("startHostDispatchBrokerRequest: %v", err)
	}
	if logPath == "" {
		t.Fatal("startHostDispatchBrokerRequest returned an empty dispatch artifact path")
	}
	select {
	case <-recoveryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("missing engineer visibility never reached asynchronous recovery")
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("missing engineer visibility never finalized its artifact")
	}
	summary := waitForDispatchArtifactSummary(t,
		filepath.Join(filepath.Dir(logPath), dispatchArtifactSummaryFile),
		"outcome: failed-before-container", "ward agent list")
	for _, want := range []string{"outcome: failed-before-container", "ward agent list"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("dispatch summary missing %q:\n%s", want, summary)
		}
	}
}

func TestStartHostDispatchBrokerRequestReportsCrossOwnerVisibilityCollisionAsynchronously(t *testing.T) {
	setTestHome(t, t.TempDir())
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	collidingName := "engineer-codex-website-66"
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = ps ]; then\n" +
		"  for arg in \"$@\"; do\n" +
		"    case \"$arg\" in\n" +
		"      name=^" + collidingName + "$)\n" +
		"        printf '%s\\n' " + shellQuote(collidingName) + "\n" +
		"        exit 0\n" +
		"        ;;\n" +
		"    esac\n" +
		"  done\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	r := &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}

	origLaunch := dispatchBrokerLaunch
	origTimeout := dispatchBrokerVisibilityTimeout
	origPoll := dispatchBrokerVisibilityPoll
	origFailedHook := dispatchFailedDispatchLaunchHook
	origRestoreHook := dispatchStdioRestoreHook
	recoveryStarted := make(chan struct{})
	finished := make(chan struct{})
	t.Cleanup(func() {
		dispatchBrokerLaunch = origLaunch
		dispatchBrokerVisibilityTimeout = origTimeout
		dispatchBrokerVisibilityPoll = origPoll
		dispatchFailedDispatchLaunchHook = origFailedHook
		dispatchStdioRestoreHook = origRestoreHook
	})
	dispatchBrokerVisibilityTimeout = 75 * time.Millisecond
	dispatchBrokerVisibilityPoll = 10 * time.Millisecond
	dispatchFailedDispatchLaunchHook = func(dispatchBrokerRequest, string, error) bool {
		select {
		case <-recoveryStarted:
		default:
			close(recoveryStarted)
		}
		return true
	}
	dispatchStdioRestoreHook = func() {
		select {
		case <-finished:
		default:
			close(finished)
		}
	}
	dispatchBrokerLaunch = func(context.Context, dispatchBrokerRequest) error { return nil }

	req := dispatchBrokerRequest{
		Role:      "engineer",
		Argv:      []string{"engineer", "coilysiren/website#66", "--harness", "codex", "--pr"},
		Requester: "director-codex-host",
		Token:     "nonce-collision",
	}
	logPath, err := r.startHostDispatchBrokerRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("startHostDispatchBrokerRequest: %v", err)
	}
	if logPath == "" {
		t.Fatal("startHostDispatchBrokerRequest returned an empty dispatch artifact path")
	}
	select {
	case <-recoveryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("cross-owner visibility collision never reached asynchronous recovery")
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("cross-owner visibility collision never finalized its artifact")
	}
	summary := waitForDispatchArtifactSummary(t,
		filepath.Join(filepath.Dir(logPath), dispatchArtifactSummaryFile),
		"outcome: failed-before-container", "ward agent list")
	if !strings.Contains(summary, "ward agent list") {
		t.Fatalf("dispatch summary = %q, want the director-surface follow-up command", summary)
	}
}

func TestRedactDispatchBrokerArgvKeepsWorkflowAndDetailsButScrubsSecrets(t *testing.T) {
	got := redactDispatchBrokerArgv([]string{
		"engineer", "coilyco-flight-deck/ward#1",
		"--workflow", "merge-remote-main",
		"--details", "repair after PR #357",
		"--config", "agent.claude.api-key=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	for _, want := range []string{"--workflow merge-remote-main", "--details repair after PR #357"} {
		if !strings.Contains(got, want) {
			t.Errorf("redacted argv %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Errorf("redacted argv leaked a secret-shaped value: %q", got)
	}
}

// TestDispatchLogNameIsStampedAndAttributable locks the ward#389 log basename: a UTC
// minute stamp for sortable re-dispatches plus a filesystem-safe requester + ref slug.
func TestDispatchLogNameIsStampedAndAttributable(t *testing.T) {
	at := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	req := dispatchBrokerRequest{
		Requester: "director-claude-ward-x",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#389", "--agent", "claude"},
	}
	got := dispatchLogName(req, at)
	want := "20260701T120000Z-director-claude-ward-x-coilyco-flight-deck-ward-389.log"
	if got != want {
		t.Errorf("dispatchLogName() = %q, want %q", got, want)
	}
	// A requester-less request still yields a sane, collision-free basename.
	bare := dispatchLogName(dispatchBrokerRequest{Argv: []string{"qa"}}, at)
	if !strings.HasPrefix(bare, "20260701T120000Z-unknown") || !strings.HasSuffix(bare, ".log") {
		t.Errorf("requester-less dispatchLogName() = %q, want stamped unknown-*.log", bare)
	}
}

func TestDispatchArtifactPersistsMetaSummaryAndLookup(t *testing.T) {
	setTestHome(t, t.TempDir())
	at := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	req := dispatchBrokerRequest{
		Requester: "director-codex-host",
		Role:      "engineer",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#389", "--harness", "codex"},
	}
	paths, logf, err := openDispatchArtifact(req, at, "deadbeef")
	if err != nil {
		t.Fatalf("openDispatchArtifact: %v", err)
	}
	if _, err := fmt.Fprintln(logf, "ward dispatch broker: launch failed: Conflict. The container name \"engineer-codex-ward-389\" is already in use"); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}
	if err := logf.Close(); err != nil {
		t.Fatalf("close dispatch log: %v", err)
	}
	finalizeDispatchArtifact(paths, req, paths.ConsolePath, errors.New(`Conflict. The container name "/engineer-codex-ward-389" is already in use`))

	body, err := os.ReadFile(paths.MetaPath)
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	if !strings.Contains(string(body), `"request_id": "deadbeef"`) {
		t.Fatalf("meta.json missing request id:\n%s", body)
	}
	if !strings.Contains(string(body), `"outcome": "failed-before-container"`) {
		t.Fatalf("meta.json missing failed outcome:\n%s", body)
	}
	if !strings.Contains(string(body), `"error_class": "launch-failure"`) {
		t.Fatalf("meta.json missing error class:\n%s", body)
	}
	summary, err := os.ReadFile(paths.SummaryPath)
	if err != nil {
		t.Fatalf("read summary.md: %v", err)
	}
	if !strings.Contains(string(summary), "request_id: deadbeef") {
		t.Fatalf("summary missing request id:\n%s", summary)
	}
	if !strings.Contains(string(summary), "already in use") {
		t.Fatalf("summary missing failure detail:\n%s", summary)
	}
	if got, ok, err := latestDispatchConsolePathForRef(agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 389}); err != nil || !ok {
		t.Fatalf("latestDispatchConsolePathForRef: ok=%v err=%v", ok, err)
	} else if got != paths.ConsolePath {
		t.Fatalf("latestDispatchConsolePathForRef() = %q, want %q", got, paths.ConsolePath)
	}
	r := &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(string) (string, error) { return "/bin/true", nil },
	}}
	src, err := r.resolveAgentLogsSourceForName(t.Context(), "engineer", 0, false)
	if err != nil {
		t.Fatalf("resolveAgentLogsSourceForName: %v", err)
	}
	if src.Kind != agentLogSourceFile || src.Path != paths.ConsolePath {
		t.Fatalf("role lookup resolved %#v, want dispatch artifact %q", src, paths.ConsolePath)
	}
}

func TestStartHostDispatchBrokerRequestDecisionArtifactShape(t *testing.T) {
	setTestHome(t, t.TempDir())
	r := fakeEngineerVisibilityDockerRunner(t, "engineer-codex-ward-1469", 1)

	origLaunch := dispatchBrokerLaunch
	origRestoreHook := dispatchStdioRestoreHook
	finished := make(chan struct{})
	t.Cleanup(func() {
		dispatchBrokerLaunch = origLaunch
		dispatchStdioRestoreHook = origRestoreHook
	})
	dispatchStdioRestoreHook = func() {
		select {
		case <-finished:
		default:
			close(finished)
		}
	}
	dispatchBrokerLaunch = func(_ context.Context, req dispatchBrokerRequest) error {
		if got := brokeredDispatchRequestID(); got != req.RequestID {
			t.Fatalf("brokered launch env request id = %q, want %q", got, req.RequestID)
		}
		logDispatchDecision(os.Stderr, "host", "plan", "container=engineer-codex-ward-1469 image=ward/dev-base")
		maybeDumpSeed(os.Stderr, strings.Repeat("full issue body should not dominate\n", 24), false)
		return nil
	}

	req := dispatchBrokerRequest{
		Role:      "engineer",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1469", "--harness", "codex"},
		Requester: "director-codex-host",
		Token:     "nonce-shape",
	}
	logPath, err := r.startHostDispatchBrokerRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("startHostDispatchBrokerRequest: %v", err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch artifact was not finalized")
	}
	waitForDispatchArtifactSummary(t, filepath.Join(filepath.Dir(logPath), dispatchArtifactSummaryFile), "outcome: launched")

	raw, err := os.ReadFile(logPath) // #nosec G304 -- test-owned artifact path
	if err != nil {
		t.Fatalf("read raw dispatch console: %v", err)
	}
	redactedPath := filepath.Join(agentLogsRedactedDir(), dispatchArtifactsSubdir, filepath.Base(filepath.Dir(logPath)), dispatchArtifactRedactedConsole)
	redacted, err := os.ReadFile(redactedPath) // #nosec G304 -- test-owned artifact path
	if err != nil {
		t.Fatalf("read redacted dispatch console: %v", err)
	}
	for _, body := range []string{string(raw), string(redacted)} {
		for _, want := range []string{
			"ward dispatch decision: component=broker checkpoint=request-accepted",
			"ward dispatch decision: component=broker checkpoint=backpressure-open-pr passed",
			"ward dispatch decision: component=host checkpoint=plan container=engineer-codex-ward-1469",
			"ward dispatch decision: component=broker checkpoint=visibility confirmed",
			"----- seeded prompt summary -----",
			"seed omitted from this host decision log",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("dispatch console missing %q:\n%s", want, body)
			}
		}
		for _, unwanted := range []string{"----- seeded prompt -----", "full issue body should not dominate"} {
			if strings.Contains(body, unwanted) {
				t.Fatalf("dispatch console should collapse seed payload %q:\n%s", unwanted, body)
			}
		}
	}
}

func TestStartHostDispatchBrokerRequestDeferredDecisionArtifact(t *testing.T) {
	setTestHome(t, t.TempDir())
	oldBase := forgejoBaseURL
	t.Cleanup(func() { forgejoBaseURL = oldBase })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "pulls" {
			t.Fatalf("unexpected issue feed query: %s", r.URL.RawQuery)
		}
		rows := make([]map[string]any, 0, 7)
		for i := 1; i <= 7; i++ {
			rows = append(rows, map[string]any{
				"number":       i,
				"title":        "PR",
				"body":         "body",
				"state":        "open",
				"html_url":     "https://forgejo.example/coilyco-flight-deck/ward/pulls/1",
				"pull_request": map[string]any{"url": "https://forgejo.example/coilyco-flight-deck/ward/pulls/1"},
				"labels":       []map[string]any{},
			})
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	for i := 1; i <= 7; i++ {
		i := i
		mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/"+strconv.Itoa(i), func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"mergeable": true})
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	origLaunch := dispatchBrokerLaunch
	origRestoreHook := dispatchStdioRestoreHook
	finished := make(chan struct{})
	t.Cleanup(func() {
		dispatchBrokerLaunch = origLaunch
		dispatchStdioRestoreHook = origRestoreHook
	})
	dispatchBrokerLaunch = func(context.Context, dispatchBrokerRequest) error {
		t.Fatal("launch should not run after broker-time open PR backpressure")
		return nil
	}
	dispatchStdioRestoreHook = func() {
		select {
		case <-finished:
		default:
			close(finished)
		}
	}

	req := dispatchBrokerRequest{
		Role:      "engineer",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1470", "--harness", "codex"},
		Requester: "director-codex-host",
		Token:     "nonce-deferred",
	}
	logPath, err := (&Runner{}).startHostDispatchBrokerRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("startHostDispatchBrokerRequest: %v", err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred dispatch artifact was not finalized")
	}
	summary := waitForDispatchArtifactSummary(t,
		filepath.Join(filepath.Dir(logPath), dispatchArtifactSummaryFile),
		"outcome: deferred-open-pr", "open-pr-backpressure")
	if !strings.Contains(summary, "outcome: deferred-open-pr") {
		t.Fatalf("summary missing deferred-open-pr:\n%s", summary)
	}
	raw, err := os.ReadFile(logPath) // #nosec G304 -- test-owned artifact path
	if err != nil {
		t.Fatalf("read deferred dispatch console: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"ward dispatch decision: component=broker checkpoint=request-accepted",
		"ward dispatch decision: component=broker checkpoint=backpressure-open-pr checking",
		"ward dispatch decision: component=broker checkpoint=backpressure-open-pr deferred",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deferred dispatch console missing %q:\n%s", want, body)
		}
	}
}

func fakeAgentLogsDockerRunner(t *testing.T, psOut, logsOut string, cpOut []byte, wantCpSuffix string) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	cpPath := ""
	if len(cpOut) > 0 {
		cpPath = filepath.Join(dir, "cp.tar")
		if err := os.WriteFile(cpPath, cpOut, 0o600); err != nil {
			t.Fatalf("write fake docker cp tar: %v", err)
		}
	}
	body := "#!/bin/sh\n" +
		"want_cp_suffix=" + shellQuote(wantCpSuffix) + "\n" +
		"if [ \"$1\" = ps ] && [ \"$2\" = -a ]; then\n" +
		"  printf '%s' " + shellQuote(psOut) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{index .Config.Labels \"ward.role\"}}' ]; then\n" +
		"  case " + shellQuote(strings.TrimSpace(psOut)) + " in\n" +
		"    director-*) printf '%s\\n' director ;;\n" +
		"    *) printf '%s\\n' engineer ;;\n" +
		"  esac\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .Config.Env}}' ]; then\n" +
		"  printf '%s' " + shellQuote(`["WARD_AGENT_HOME=/home/ubuntu/.ward"]`) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = logs ]; then\n" +
		"  printf '%s' " + shellQuote(logsOut) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = cp ]; then\n" +
		"  if [ -n \"$want_cp_suffix\" ]; then\n" +
		"    case \"$2\" in\n" +
		"      *\"$want_cp_suffix\") ;;\n" +
		"      *) printf '%s\\n' \"unexpected docker cp source: $2\" >&2; exit 1;;\n" +
		"    esac\n" +
		"  fi\n" +
		"  if [ -n " + shellQuote(testShellPath(cpPath)) + " ]; then\n" +
		"    cat " + shellQuote(testShellPath(cpPath)) + "\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}

func fakeEngineerVisibilityDockerRunner(t *testing.T, visibleName string, visibleAfter int) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	countPath := filepath.Join(dir, "count")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = ps ]; then\n" +
		"  count=0\n" +
		"  if [ -f " + shellQuote(testShellPath(countPath)) + " ]; then\n" +
		"    count=$(cat " + shellQuote(testShellPath(countPath)) + ")\n" +
		"  fi\n" +
		"  count=$((count + 1))\n" +
		"  printf '%s' \"$count\" > " + shellQuote(testShellPath(countPath)) + "\n" +
		"  if [ \"$count\" -ge " + fmt.Sprintf("%d", visibleAfter) + " ] && [ -n " + shellQuote(visibleName) + " ]; then\n" +
		"    printf '%s\\n' " + shellQuote(visibleName) + "\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}

func fakeStopDockRunner(t *testing.T, visibleName string) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  ps)\n" +
		"    if [ -n " + shellQuote(visibleName) + " ]; then\n" +
		"      printf '%s\\n' " + shellQuote(visibleName) + "\n" +
		"    fi\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"  inspect)\n" +
		"    if [ -n " + shellQuote(visibleName) + " ]; then\n" +
		"      printf '%s\\n' " + shellQuote(roleEngineer) + "\n" +
		"    fi\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"  stop)\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n" +
		"printf '%s\\n' \"unexpected docker args: $*\" >&2\n" +
		"exit 1\n"
	writeTestShellCommand(t, script, body)
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(_ string) (string, error) { return script, nil },
	}}
}

func liveTranscriptTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(body)), Mode: 0o644}); err != nil {
			t.Fatalf("write tar header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("write tar body for %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

// TestServedRunStdioLandsInLogNotTTY is the ward#389 regression: the redirect routes a
// served run's os.Stdout/os.Stderr bytes into the per-dispatch log, then restores them.
func TestServedRunStdioLandsInLogNotTTY(t *testing.T) {
	setTestHome(t, t.TempDir())
	req := dispatchBrokerRequest{
		Requester: "director-claude-ward-1",
		Argv:      []string{"engineer", "coilyco-flight-deck/ward#1", "--agent", "claude"},
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
	fmt.Fprint(os.Stderr, "director-claude-ward-1: pulling some-image\n")
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
