package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// ward#349: --ts-sidecar attaches the carry to the shared ward-tailnet network
// (--network=ward-tailnet); off by default no --network is passed.
func TestDockerArgvTSSidecar(t *testing.T) {
	// Default plan: no --network at all.
	if joined := strings.Join(dockerCreateArgv(sampleUpPlan(), ""), " "); strings.Contains(joined, "--network") {
		t.Errorf("default run must not pass --network; got: %s", joined)
	}

	p := sampleUpPlan()
	p.TSSidecar = true
	want := "--network=" + wardTailnetNetwork
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	if !strings.Contains(joined, want) {
		t.Errorf("--ts-sidecar run must pass %q; got: %s", want, joined)
	}
	// It joins ward-tailnet, never the host network or a per-run sidecar netns.
	if strings.Contains(joined, "--network=host") {
		t.Errorf("--ts-sidecar must not pass --network=host; got: %s", joined)
	}
	if strings.Contains(joined, "--network=container:") {
		t.Errorf("--ts-sidecar must not join a per-run sidecar netns; got: %s", joined)
	}
	// The flag rides the shared head, so the no-binds (create) builder carries it too.
	if j := strings.Join(dockerCreateNoBindsArgv(p, ""), " "); !strings.Contains(j, want) {
		t.Errorf("--ts-sidecar create must pass %q; got: %s", want, j)
	}
}

// TestDockerTailnetInspectArgv: the preflight reads the names attached to the
// ward-tailnet network; the inspect fails (non-zero) when the network is absent.
func TestDockerTailnetInspectArgv(t *testing.T) {
	joined := strings.Join(dockerTailnetInspectArgv(), " ")
	for _, want := range []string{"network", "inspect", wardTailnetNetwork, "{{range .Containers}}{{.Name}} {{end}}"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tailnet inspect argv missing %q; got: %s", want, joined)
		}
	}
}

// TestProxyBoxAttached: the standing box is detected among the network's attached
// container names; an absent box (or empty output, the missing-network case) is not.
func TestProxyBoxAttached(t *testing.T) {
	if !proxyBoxAttached("some-carry " + proxyBoxName + " other-carry ") {
		t.Errorf("proxyBoxAttached should find %q among attached names", proxyBoxName)
	}
	if proxyBoxAttached("some-carry other-carry ") {
		t.Error("proxyBoxAttached must be false when the box is not attached")
	}
	// Empty inspect output (the network does not exist) -> not attached.
	if proxyBoxAttached("") {
		t.Error("proxyBoxAttached must be false on empty output (missing network)")
	}
	// A substring of the box name must not false-match.
	if proxyBoxAttached("mac-proxy-staging ") {
		t.Error("proxyBoxAttached must match the box name exactly, not as a substring")
	}
}

// TestTSSidecarWardEnv: a --ts-sidecar carry is told the socks5h proxy by the box's
// name + the by-name tower endpoint, never ALL_PROXY; a default carry, neither.
func TestTSSidecarWardEnv(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_TS_SOCKS5"]; ok {
		t.Error("default carry must not set WARD_TS_SOCKS5")
	}
	if _, ok := p.wardEnv()["WARD_TOWER_OLLAMA"]; ok {
		t.Error("default carry must not set WARD_TOWER_OLLAMA")
	}
	p.TSSidecar = true
	env := p.wardEnv()
	// socks5h://mac-proxy:1055 - the box dialed by name, socks5h so it resolves the
	// tower's MagicDNS name tailnet-side (ward#349; the doc).
	if got, want := env["WARD_TS_SOCKS5"], "socks5h://"+proxyBoxHost; got != want {
		t.Errorf("WARD_TS_SOCKS5 = %q, want %q", got, want)
	}
	if !strings.Contains(env["WARD_TS_SOCKS5"], proxyBoxName) {
		t.Errorf("WARD_TS_SOCKS5 must dial the box by name %q; got %q", proxyBoxName, env["WARD_TS_SOCKS5"])
	}
	// The proxy is reached by name, never loopback (the box is not a netns peer now).
	if strings.Contains(env["WARD_TS_SOCKS5"], "127.0.0.1") {
		t.Errorf("WARD_TS_SOCKS5 must dial the box by name, not loopback; got %q", env["WARD_TS_SOCKS5"])
	}
	// The tower endpoint is the MagicDNS name (by name, no SSM IP), dialed :11434.
	if got := env["WARD_TOWER_OLLAMA"]; got != towerOllamaURL {
		t.Errorf("WARD_TOWER_OLLAMA = %q, want %q", got, towerOllamaURL)
	}
	if !strings.Contains(env["WARD_TOWER_OLLAMA"], towerMagicDNSName) {
		t.Errorf("WARD_TOWER_OLLAMA must dial the tower by MagicDNS name %q; got %q", towerMagicDNSName, env["WARD_TOWER_OLLAMA"])
	}
	// Per-connection only: never a blanket ALL_PROXY (the proxy reaches the tailnet,
	// not the public internet, so routing everything through it would break egress).
	for k := range env {
		if strings.EqualFold(k, "ALL_PROXY") {
			t.Errorf("a --ts-sidecar carry must not set %s (per-connection, not host-wide)", k)
		}
	}
}

// TestCredEnvLinesNoTower: the tower endpoint is no longer a base64'd cred line - it
// dials by MagicDNS name (a non-secret), so it rides plain in wardEnv (ward#337).
func TestCredEnvLinesNoTower(t *testing.T) {
	lines := credEnvLines(agentCreds{Claude: "blob", GooseOllamaHost: "http://h:11434"})
	for _, l := range lines {
		if strings.Contains(l, "WARD_TOWER_OLLAMA") {
			t.Errorf("tower endpoint must not ride the cred env-file; got: %v", lines)
		}
	}
	if got := credEnvLines(agentCreds{}); len(got) != 0 {
		t.Errorf("no creds -> no lines; got: %v", got)
	}
}

// fakeDockerRunner builds a Runner whose "docker" resolves to a tiny shell script
// emitting `stdout` and exiting `code`, so the preflight can be exercised offline.
func fakeDockerRunner(t *testing.T, stdout string, code int) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\nprintf '%s' " + shellQuote(stdout) + "\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write fake docker: %v", err)
	}
	return &Runner{Runner: &shell.Runner{
		Stderr:  io.Discard,
		Resolve: func(bin string) (string, error) { return script, nil },
	}}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// TestPreflightTailnetProxy covers ward#349: the standing box must be attached to
// ward-tailnet; a missing network (inspect fails) or an unattached box is a clear error.
func TestPreflightTailnetProxy(t *testing.T) {
	ctx := context.Background()

	// Network exists and the box is attached -> the preflight passes.
	if err := fakeDockerRunner(t, "some-carry "+proxyBoxName+" ", 0).preflightTailnetProxy(ctx); err != nil {
		t.Errorf("box attached: preflight should pass; got: %v", err)
	}

	// Missing network: `docker network inspect` exits non-zero -> the clear error.
	err := fakeDockerRunner(t, "Error: No such network: ward-tailnet\n", 1).preflightTailnetProxy(ctx)
	if err == nil {
		t.Fatal("missing network: preflight should error")
	}
	for _, want := range []string{"standing tailnet proxy not found", "agentic-os#291"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing-network error %q must contain %q", err, want)
		}
	}

	// Network exists but the box is not attached -> the same clear error.
	if err := fakeDockerRunner(t, "some-other-carry ", 0).preflightTailnetProxy(ctx); err == nil {
		t.Error("box unattached: preflight should error")
	} else if !strings.Contains(err.Error(), "standing tailnet proxy not found") {
		t.Errorf("box-unattached error %q must name the standing proxy", err)
	}
}

// The buildUpPlan tailnet plumbing is covered by TestBuildUpPlanTailnet in
// container_hostnet_test.go now the two escalations are one --tailnet flag (ward#362).
