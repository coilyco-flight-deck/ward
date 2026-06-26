package main

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
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
	dir := t.TempDir()
	socket := filepath.Join(dir, "broker.sock")
	ln, err := net.Listen("unix", socket)
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

	t.Setenv(envDispatchBrokerSocket, socket)
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
	want := []string{"engineer", "coilyco-flight-deck/ward#378", "--driver", "claude", "--no-preflight"}
	if !reflect.DeepEqual(req.Argv, want) {
		t.Errorf("forwarded argv = %v, want %v", req.Argv, want)
	}
}

func TestDispatchBrokerEnvAndMountArePlanLocal(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()[envDispatchBrokerSocket]; ok {
		t.Fatal("direct host dispatch plan unexpectedly has a dispatch broker env")
	}
	p.DispatchBrokerSock = containerDispatchBrokerSock
	if got := p.wardEnv()[envDispatchBrokerSocket]; got != containerDispatchBrokerSock {
		t.Errorf("broker env = %q, want %q", got, containerDispatchBrokerSock)
	}
	m := dispatchBrokerMount("/tmp/host-broker.sock")
	if m.Source != "/tmp/host-broker.sock" || m.Target != containerDispatchBrokerSock || m.ReadOnly || m.Volume {
		t.Errorf("dispatchBrokerMount = %+v", m)
	}
}

func TestNoBrokerKeepsDirectDispatchPath(t *testing.T) {
	t.Setenv(envDispatchBrokerSocket, "")
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

func parseCommandForTest(t *testing.T, flags []cli.Flag, argv []string) *cli.Command {
	t.Helper()
	cmd := &cli.Command{Name: argv[0], Flags: flags, Action: func(context.Context, *cli.Command) error { return nil }}
	if err := cmd.Run(t.Context(), argv); err != nil {
		t.Fatalf("parse %s: %v", strings.Join(argv, " "), err)
	}
	return cmd
}
