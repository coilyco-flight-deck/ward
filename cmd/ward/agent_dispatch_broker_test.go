package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"

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
	ok := dispatchBrokerRequest{Role: "engineer", Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--driver", "claude"}}
	if err := validateDispatchBrokerRequest(ok); err != nil {
		t.Errorf("valid engineer dispatch refused: %v", err)
	}
	advisor := dispatchBrokerRequest{Role: "advisor", Argv: []string{"advisor", "coilyco-flight-deck/ward#1", "--driver", "goose", "what changed?"}}
	if err := validateDispatchBrokerRequest(advisor); err != nil {
		t.Errorf("valid advisor dispatch refused: %v", err)
	}
}

func TestBrokerEngineerArgvForwardsApprovedFlags(t *testing.T) {
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#42",
		"--driver", "claude",
		"--image", "img", "--tag", "t1", "--ward-version", "v1",
		"--repo", "coilyco-flight-deck/cli-guard",
		"--aws", "--ts-sidecar", "--force", "--no-preflight",
	})
	got := brokerEngineerArgv(cmd, modeClaude, agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 42})
	for _, want := range [][]string{
		{"--driver", "claude"},
		{"--image", "img"},
		{"--tag", "t1"},
		{"--ward-version", "v1"},
		{"--repo", "coilyco-flight-deck/cli-guard"},
	} {
		if !argFollowedBy(got, want[0], want[1]) {
			t.Errorf("forwarded argv missing %s %s: %v", want[0], want[1], got)
		}
	}
	for _, want := range []string{"engineer", "coilyco-flight-deck/ward#42", "--aws", "--ts-sidecar", "--force", "--no-preflight"} {
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
		"engineer", "coilyco-flight-deck/ward#378", "--driver", "claude", "--no-preflight",
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
	want := []string{"engineer", "coilyco-flight-deck/ward#378", "--driver", "claude", "--no-preflight"}
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

	err = sendDispatchBrokerRequest(t.Context(), addr, dispatchBrokerRequest{Role: "engineer"})
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

	err = sendDispatchBrokerRequest(t.Context(), ln.Addr().String(), dispatchBrokerRequest{Role: "engineer"})
	if err == nil {
		t.Fatal("credential-broker reply unexpectedly accepted")
	}
	if !strings.Contains(err.Error(), "wrong broker") {
		t.Errorf("error %q does not carry the wrong-broker hint", err)
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
