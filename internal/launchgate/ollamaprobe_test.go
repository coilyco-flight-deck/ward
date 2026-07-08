package ollamaprobe

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func noopLog(string, ...any) {}

// TestDialAddr normalises URLs and bare host[:port] forms, defaulting scheme+port.
func TestDialAddr(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"http://localhost:11434/v1", "localhost:11434", false},
		{"http://tower:11434", "tower:11434", false},
		{"tower:11434", "tower:11434", false},
		{"localhost", "localhost:11434", false}, // bare host -> default port
		{"http://tower", "tower:11434", false},  // URL without port -> default port
		{"  http://tower:9999  ", "tower:9999", false},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := dialAddr(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("dialAddr(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("dialAddr(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("dialAddr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestReachOnce covers the host-side single-dial probe: a live listener returns its
// addr, a closed port and a malformed endpoint each error.
func TestReachOnce(t *testing.T) {
	savedDial := dialTimeout
	dialTimeout = 100 * time.Millisecond
	defer func() { dialTimeout = savedDial }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	live := ln.Addr().String()
	addr, err := ReachOnce(context.Background(), "http://"+live+"/v1")
	if err != nil {
		t.Errorf("ReachOnce against a live listener should pass, got %v", err)
	}
	if addr != live {
		t.Errorf("ReachOnce addr = %q, want %q", addr, live)
	}

	// A bound-then-closed port is guaranteed refused, not merely idle.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	dead := ln2.Addr().String()
	_ = ln2.Close()
	if _, err := ReachOnce(context.Background(), "http://"+dead); err == nil {
		t.Error("ReachOnce against a closed port should error")
	}

	// A malformed endpoint errors at parse, before any dial.
	if _, err := ReachOnce(context.Background(), "   "); err == nil {
		t.Error("ReachOnce with an empty endpoint should error at parse")
	}
	_ = ln.Close()
}

// TestPreLaunchNoOps confirms the gate stands aside when there is nothing to guard:
// a non-headless run, the skip switch, or no endpoint - none is the backend down.
func TestPreLaunchNoOps(t *testing.T) {
	// Non-headless: interactive runs have a human watching, so no gate.
	if err := PreLaunch(agentsapi.RunCtx{Headless: false, Log: noopLog}, "goose", "http://127.0.0.1:1/v1"); err != nil {
		t.Errorf("non-headless PreLaunch should no-op, got %v", err)
	}
	// Skip switch bypasses even a headless run against a dead endpoint.
	t.Setenv(skipEnv, "1")
	if err := PreLaunch(agentsapi.RunCtx{Headless: true, Log: noopLog}, "goose", "http://127.0.0.1:1/v1"); err != nil {
		t.Errorf("%s=1 should bypass the probe, got %v", skipEnv, err)
	}
	t.Setenv(skipEnv, "0")
	// Empty endpoint: nothing to probe.
	if err := PreLaunch(agentsapi.RunCtx{Headless: true, Log: noopLog}, "opencode", "   "); err != nil {
		t.Errorf("empty endpoint should no-op, got %v", err)
	}
}

// TestPreLaunchReachable passes against a live listener (the happy path).
func TestPreLaunchReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	rc := agentsapi.RunCtx{Ctx: context.Background(), Headless: true, Log: noopLog}
	if err := PreLaunch(rc, "opencode", "http://"+ln.Addr().String()+"/v1"); err != nil {
		t.Errorf("PreLaunch against a live listener should pass, got %v", err)
	}
}

// TestPreLaunchUnreachable aborts against a closed port, naming the endpoint and
// the bypass. The window is shrunk so the retry loop does not stall the test.
func TestPreLaunchUnreachable(t *testing.T) {
	savedWindow, savedDial, savedRetry := probeWindow, dialTimeout, retryDelay
	probeWindow, dialTimeout, retryDelay = 300*time.Millisecond, 100*time.Millisecond, 20*time.Millisecond
	defer func() { probeWindow, dialTimeout, retryDelay = savedWindow, savedDial, savedRetry }()

	// Bind then close a listener so the port is guaranteed refused, not merely idle.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	rc := agentsapi.RunCtx{Ctx: context.Background(), Headless: true, Log: noopLog}
	err = PreLaunch(rc, "goose", "http://"+addr)
	if err == nil {
		t.Fatal("PreLaunch against a closed port should error")
	}
	for _, want := range []string{"unreachable", addr, "goose", skipEnv} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}
