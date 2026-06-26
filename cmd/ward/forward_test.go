package main

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// ward#359: parseSocks5ProxyAddr strips the socks5h://|socks5:// scheme and any
// trailing path, leaving the bare host:port the proxy listens on.
func TestParseSocks5ProxyAddr(t *testing.T) {
	cases := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{"socks5h://mac-proxy:1055", "mac-proxy:1055", false},
		{"socks5://mac-proxy:1055", "mac-proxy:1055", false},
		{"socks5h://mac-proxy:1055/kai-tower-3026:11434", "mac-proxy:1055", false},
		{"mac-proxy:1055", "mac-proxy:1055", false},
		{"  socks5h://mac-proxy:1055  ", "mac-proxy:1055", false},
		{"127.0.0.1:1055", "127.0.0.1:1055", false},
		{"", "", true},
		{"socks5h://", "", true},
		{"socks5h://mac-proxy", "", true}, // no port
	}
	for _, tc := range cases {
		got, err := parseSocks5ProxyAddr(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSocks5ProxyAddr(%q): want error, got %q", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSocks5ProxyAddr(%q): unexpected error %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSocks5ProxyAddr(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// The forwarder's no-proxy endpoint constants must agree with the tower port the
// proxy vars already use, so localhost:11434 truly shadows the tower (ward#359).
func TestForwardEndpointConstants(t *testing.T) {
	if !strings.HasSuffix(forwardListenAddr, ":"+towerOllamaPort) {
		t.Errorf("forwardListenAddr %q must listen on the tower port %q", forwardListenAddr, towerOllamaPort)
	}
	if !strings.HasPrefix(forwardListenAddr, "127.0.0.1:") {
		t.Errorf("forwardListenAddr %q must bind loopback only (no capability, no external exposure)", forwardListenAddr)
	}
	if towerOllamaLocalURL != "http://localhost:"+towerOllamaPort {
		t.Errorf("towerOllamaLocalURL = %q, want http://localhost:%s", towerOllamaLocalURL, towerOllamaPort)
	}
}

// A --ts-sidecar plan must export WARD_TOWER_OLLAMA_LOCAL (the no-proxy endpoint)
// alongside the existing explicit-proxy vars; a non-sidecar plan must not.
func TestWardEnvTowerOllamaLocal(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_TOWER_OLLAMA_LOCAL"]; ok {
		t.Errorf("non-ts-sidecar plan must not export WARD_TOWER_OLLAMA_LOCAL")
	}

	p.TSSidecar = true
	env := p.wardEnv()
	if got := env["WARD_TOWER_OLLAMA_LOCAL"]; got != towerOllamaLocalURL {
		t.Errorf("WARD_TOWER_OLLAMA_LOCAL = %q, want %q", got, towerOllamaLocalURL)
	}
	// The explicit --proxy path stays valid: both old vars remain.
	if env["WARD_TS_SOCKS5"] == "" || env["WARD_TOWER_OLLAMA"] == "" {
		t.Errorf("--ts-sidecar must keep WARD_TS_SOCKS5 + WARD_TOWER_OLLAMA: %v", env)
	}
}

// fakeSOCKS5 is a minimal no-auth SOCKS5 CONNECT server: it answers the greeting,
// reads a domain CONNECT, dials it locally, bridges, and records the asked domain.
type fakeSOCKS5 struct {
	ln          net.Listener
	gotDomain   chan string
	dialTo      string // override CONNECT target (the real local backend)
	refuseConn  bool   // reply 0x05 (connection refused) instead of bridging
	refuseGreet bool   // choose method 0xFF (no acceptable auth)
}

func newFakeSOCKS5(t *testing.T, dialTo string) *fakeSOCKS5 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake socks5 listen: %v", err)
	}
	f := &fakeSOCKS5{ln: ln, gotDomain: make(chan string, 1), dialTo: dialTo}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeSOCKS5) addr() string { return f.ln.Addr().String() }

func (f *fakeSOCKS5) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(c)
	}
}

func (f *fakeSOCKS5) handle(c net.Conn) {
	defer c.Close()
	greet := make([]byte, 3)
	if _, err := io.ReadFull(c, greet); err != nil {
		return
	}
	if f.refuseGreet {
		_, _ = c.Write([]byte{0x05, 0xFF})
		return
	}
	_, _ = c.Write([]byte{0x05, 0x00}) // no-auth chosen
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		return
	}
	if head[3] != 0x03 { // this fake only speaks domain addresses (socks5h)
		_, _ = c.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	lb := make([]byte, 1)
	if _, err := io.ReadFull(c, lb); err != nil {
		return
	}
	dom := make([]byte, int(lb[0]))
	if _, err := io.ReadFull(c, dom); err != nil {
		return
	}
	if _, err := io.ReadFull(c, make([]byte, 2)); err != nil { // port
		return
	}
	select {
	case f.gotDomain <- string(dom):
	default:
	}
	if f.refuseConn {
		_, _ = c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	// Success reply with a bound IPv4 addr+port, then bridge to the real backend.
	_, _ = c.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})
	backend, err := net.Dial("tcp", f.dialTo)
	if err != nil {
		return
	}
	defer backend.Close()
	go func() { _, _ = io.Copy(backend, c) }()
	_, _ = io.Copy(c, backend)
}

// echoBackend is a trivial TCP server standing in for the tower's Ollama: it
// echoes whatever it receives, so a round-trip through the forwarder is verifiable.
func echoBackend(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo backend listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// End-to-end: a byte written to the loopback listener echoes back through the
// SOCKS5 CONNECT, and the proxy was asked to resolve the target domain (socks5h).
func TestServeForwardEndToEnd(t *testing.T) {
	backend := echoBackend(t)
	proxy := newFakeSOCKS5(t, backend.Addr().String())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("forwarder listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const target = "kai-tower-3026:11434"
	done := make(chan error, 1)
	go func() { done <- serveForward(ctx, ln, proxy.addr(), target, func(string, ...any) {}) }()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	msg := []byte("hello-tower")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write through forwarder: %v", err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo through forwarder: %v", err)
	}
	if string(got) != string(msg) {
		t.Errorf("round-trip = %q, want %q", got, msg)
	}

	select {
	case dom := <-proxy.gotDomain:
		if dom != "kai-tower-3026" {
			t.Errorf("proxy asked to resolve %q, want the bare tower MagicDNS name (socks5h)", dom)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxy never received a CONNECT domain")
	}

	// Cancelling the context tears the listener down cleanly (no error returned).
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serveForward after cancel returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveForward did not return after cancel")
	}
}

// A proxy that refuses the CONNECT must surface as a dial error, not a hang nor a
// silently half-open client connection.
func TestDialThroughSOCKS5Refused(t *testing.T) {
	proxy := newFakeSOCKS5(t, "")
	proxy.refuseConn = true
	_, err := dialThroughSOCKS5(proxy.addr(), "kai-tower-3026:11434", 2*time.Second)
	if err == nil {
		t.Fatal("dialThroughSOCKS5 through a refusing proxy: want error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error %q should name the SOCKS5 refusal reason", err)
	}
}

// A proxy that rejects no-auth must surface a clear handshake error.
func TestDialThroughSOCKS5GreetRefused(t *testing.T) {
	proxy := newFakeSOCKS5(t, "")
	proxy.refuseGreet = true
	_, err := dialThroughSOCKS5(proxy.addr(), "kai-tower-3026:11434", 2*time.Second)
	if err == nil {
		t.Fatal("dialThroughSOCKS5 against a no-auth-refusing proxy: want error, got nil")
	}
	if !strings.Contains(err.Error(), "no-auth") {
		t.Errorf("error %q should name the refused no-auth method", err)
	}
}

// A target that is not host:port is rejected before any dial.
func TestDialThroughSOCKS5BadTarget(t *testing.T) {
	if _, err := dialThroughSOCKS5("127.0.0.1:1055", "not-a-hostport", time.Second); err == nil {
		t.Error("dialThroughSOCKS5 with a bad target: want error, got nil")
	}
}
