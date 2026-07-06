package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// ward#349: --ts-sidecar attaches the run to the shared ward-tailnet network
// (--network=ward-tailnet); off by default no --network is passed.
func TestDockerArgvTSSidecar(t *testing.T) {
	// Default plan: no --network at all.
	if joined := strings.Join(dockerCreateArgv(sampleUpPlan(), ""), " "); strings.Contains(joined, "--network") {
		t.Errorf("default run must not pass --network; got: %s", joined)
	}

	p := sampleUpPlan()
	p.TSSidecar = true
	want := "--network=" + tailnetNetwork()
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
	for _, want := range []string{"network", "inspect", tailnetNetwork(), "{{range .Containers}}{{.Name}} {{end}}"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tailnet inspect argv missing %q; got: %s", want, joined)
		}
	}
}

// TestDockerTailnetCreateArgv: the ready-up creates the ward-tailnet network by name
// (ward#597) - the idempotent provisioning step that replaces the old hard failure.
func TestDockerTailnetCreateArgv(t *testing.T) {
	joined := strings.Join(dockerTailnetCreateArgv(), " ")
	for _, want := range []string{"network", "create", tailnetNetwork()} {
		if !strings.Contains(joined, want) {
			t.Errorf("tailnet create argv missing %q; got: %s", want, joined)
		}
	}
}

// TestProxyBoxAttached: the standing box is detected among the network's attached
// container names; an absent box (or empty output, the missing-network case) is not.
func TestProxyBoxAttached(t *testing.T) {
	if !proxyBoxAttached("some-carry " + proxyBoxName() + " other-carry ") {
		t.Errorf("proxyBoxAttached should find %q among attached names", proxyBoxName())
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

// TestProxyBoxMissingWarning: an attached box -> no warning; an absent box -> a loud
// warning naming the box, the network, and that the run still launches (ward#597).
func TestProxyBoxMissingWarning(t *testing.T) {
	if msg, warn := proxyBoxMissingWarning("some-carry " + proxyBoxName() + " "); warn {
		t.Errorf("attached box: no warning; got warn=true msg=%q", msg)
	}
	msg, warn := proxyBoxMissingWarning("some-other-carry ")
	if !warn {
		t.Fatal("unattached box: proxyBoxMissingWarning should warn")
	}
	for _, want := range []string{proxyBoxName(), tailnetNetwork(), "still launches", "ward#597"} {
		if !strings.Contains(msg, want) {
			t.Errorf("proxy-missing warning %q must contain %q", msg, want)
		}
	}
}

// TestTSSidecarWardEnv: a --ts-sidecar run is told the socks5h proxy by the box's
// name + the by-name tower endpoint, never ALL_PROXY; a default run, neither.
func TestTSSidecarWardEnv(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_TS_SOCKS5"]; ok {
		t.Error("default run must not set WARD_TS_SOCKS5")
	}
	if _, ok := p.wardEnv()["WARD_TOWER_OLLAMA"]; ok {
		t.Error("default run must not set WARD_TOWER_OLLAMA")
	}
	p.TSSidecar = true
	env := p.wardEnv()
	// socks5h://mac-proxy:1055 - the box dialed by name, socks5h so it resolves the
	// tower's MagicDNS name tailnet-side (ward#349; the doc).
	if got, want := env["WARD_TS_SOCKS5"], "socks5h://"+proxyBoxAddr(); got != want {
		t.Errorf("WARD_TS_SOCKS5 = %q, want %q", got, want)
	}
	if !strings.Contains(env["WARD_TS_SOCKS5"], proxyBoxName()) {
		t.Errorf("WARD_TS_SOCKS5 must dial the box by name %q; got %q", proxyBoxName(), env["WARD_TS_SOCKS5"])
	}
	// The proxy is reached by name, never loopback (the box is not a netns peer now).
	if strings.Contains(env["WARD_TS_SOCKS5"], "127.0.0.1") {
		t.Errorf("WARD_TS_SOCKS5 must dial the box by name, not loopback; got %q", env["WARD_TS_SOCKS5"])
	}
	// The tower endpoint is the MagicDNS name (by name, no SSM IP), dialed :11434.
	if got := env["WARD_TOWER_OLLAMA"]; got != towerOllamaURL() {
		t.Errorf("WARD_TOWER_OLLAMA = %q, want %q", got, towerOllamaURL())
	}
	if !strings.Contains(env["WARD_TOWER_OLLAMA"], towerMagicDNS()) {
		t.Errorf("WARD_TOWER_OLLAMA must dial the tower by MagicDNS name %q; got %q", towerMagicDNS(), env["WARD_TOWER_OLLAMA"])
	}
	// Per-connection only: never a blanket ALL_PROXY (the proxy reaches the tailnet,
	// not the public internet, so routing everything through it would break egress).
	for k := range env {
		if strings.EqualFold(k, "ALL_PROXY") {
			t.Errorf("a --ts-sidecar run must not set %s (per-connection, not host-wide)", k)
		}
	}
}

// TestCredEnvLinesNoTower pins that no mode's ResolveCreds emits a WARD_TOWER_OLLAMA
// key (the tower dials by MagicDNS, ward#337) and opencode injects nothing (ward#425).
func TestCredEnvLinesNoTower(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: func(string) (string, error) {
		return "", fmt.Errorf("no external binaries in this hermetic test")
	}}}
	for _, mode := range []containerMode{modeClaude, modeCodex, modeGoose, modeOpencode} {
		for _, l := range r.resolveAgentCreds(t.Context(), mode) {
			if strings.Contains(l.Key, "WARD_TOWER_OLLAMA") {
				t.Errorf("tower endpoint must not ride the cred env-file; got: %v", l)
			}
		}
	}
	if got := r.resolveAgentCreds(t.Context(), modeOpencode); len(got) != 0 {
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

// tailnetPlan is a --ts-sidecar plan carrying the given role, the input the
// preflight gates on (ward#597).
func tailnetPlan(role string) upPlan {
	p := sampleUpPlan()
	p.TSSidecar = true
	p.Role = role
	return p
}

// fakeDockerDispatch builds a Runner whose "docker" exits createCode for `network
// create`, else emits inspectOut+inspectCode; stderr is captured for the warning path.
func fakeDockerDispatch(t *testing.T, inspectOut string, inspectCode, createCode int) (*Runner, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = network ] && [ \"$2\" = create ]; then exit " + strconv.Itoa(createCode) + "; fi\n" +
		"printf '%s' " + shellQuote(inspectOut) + "\nexit " + strconv.Itoa(inspectCode) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write fake docker: %v", err)
	}
	var errbuf bytes.Buffer
	return &Runner{Runner: &shell.Runner{
		Stderr:  &errbuf,
		Resolve: func(bin string) (string, error) { return script, nil },
	}}, &errbuf
}

// TestPreflightTailnet covers ward#597 + ward#349: a non-tailnet plan no-ops; a missing
// network is created (not a failure); an unattached box warns; a failed create errors.
func TestPreflightTailnet(t *testing.T) {
	ctx := context.Background()

	// A plan that does not join the tailnet never touches docker -> always passes,
	// even with a runner that would fail the inspect.
	if err := fakeDockerRunner(t, "", 1).preflightTailnet(ctx, sampleUpPlan()); err != nil {
		t.Errorf("non-tailnet plan: preflight should no-op; got: %v", err)
	}

	// Network exists and the box is attached -> the preflight passes, no warning.
	r, errbuf := fakeDockerDispatch(t, "some-carry "+proxyBoxName()+" ", 0, 0)
	if err := r.preflightTailnet(ctx, tailnetPlan(roleAdvisor)); err != nil {
		t.Errorf("box attached: preflight should pass; got: %v", err)
	}
	if errbuf.Len() != 0 {
		t.Errorf("box attached: no warning expected; got %q", errbuf.String())
	}

	// Missing network (inspect exits non-zero), create succeeds -> ward creates it and
	// launches (ward#597). No error; a warning fires because a fresh net has no box yet.
	r, errbuf = fakeDockerDispatch(t, "Error: No such network: ward-tailnet\n", 1, 0)
	if err := r.preflightTailnet(ctx, tailnetPlan(roleAdvisor)); err != nil {
		t.Fatalf("missing network + create ok: preflight should not fail; got: %v", err)
	}
	if !strings.Contains(errbuf.String(), proxyBoxName()) || !strings.Contains(errbuf.String(), "ward#597") {
		t.Errorf("freshly-created network should warn about the unattached box; got %q", errbuf.String())
	}

	// Missing network AND create cannot land (both inspects and the create fail) -> the
	// actionable error naming the network, the by-hand create, and the fallback (ward#597).
	r, _ = fakeDockerDispatch(t, "Error: No such network: ward-tailnet\n", 1, 1)
	err := r.preflightTailnet(ctx, tailnetPlan(roleAdvisor))
	if err == nil {
		t.Fatal("missing network + create fails: preflight should error")
	}
	for _, want := range []string{tailnetNetwork(), "--no-tailnet", "ward#597"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("create-failed error %q must contain %q", err, want)
		}
	}
	// It must not misattribute the failure to the standing proxy box (the old conflation).
	if strings.Contains(err.Error(), "standing tailnet proxy not found") {
		t.Errorf("create-failed error %q must not blame the standing proxy box", err)
	}

	// Network exists but the box is not attached -> the run still launches (no error),
	// only a loud ward#597 warning (the old ward#349 hard failure is now a warning).
	r, errbuf = fakeDockerDispatch(t, "some-other-carry ", 0, 0)
	if err := r.preflightTailnet(ctx, tailnetPlan(roleAdvisor)); err != nil {
		t.Errorf("box unattached: preflight should warn, not fail; got: %v", err)
	}
	if !strings.Contains(errbuf.String(), proxyBoxName()) {
		t.Errorf("box-unattached warning must name the standing proxy; got %q", errbuf.String())
	}
}

// TestTailnetTowerEnvOverride pins ward#395: a WARD_* override wins over the baked
// literal, an unset var falls back to it, both resolvers and env reflect the override.
func TestTailnetTowerEnvOverride(t *testing.T) {
	// Unset (empty) -> the baked defaults, the fail-safe last layer.
	for k, v := range map[string]string{
		envTailnetNetwork:  "",
		envTailnetProxy:    "",
		envTowerHost:       "",
		envTowerOllamaPort: "",
	} {
		t.Setenv(k, v)
	}
	if got := tailnetNetwork(); got != defaultTailnetNetwork {
		t.Errorf("unset WARD_TAILNET_NETWORK: tailnetNetwork() = %q, want default %q", got, defaultTailnetNetwork)
	}
	if got := proxyBoxAddr(); got != defaultTailnetProxy {
		t.Errorf("unset WARD_TAILNET_PROXY: proxyBoxAddr() = %q, want default %q", got, defaultTailnetProxy)
	}
	if got, want := proxyBoxName(), "mac-proxy"; got != want {
		t.Errorf("proxyBoxName() = %q, want the host half %q of the default proxy addr", got, want)
	}
	if got := towerMagicDNS(); got != defaultTowerHost {
		t.Errorf("unset WARD_TOWER_HOST: towerMagicDNS() = %q, want default %q", got, defaultTowerHost)
	}
	if got := towerOllamaPort(); got != defaultTowerOllamaPort {
		t.Errorf("unset WARD_TOWER_OLLAMA_PORT: towerOllamaPort() = %q, want default %q", got, defaultTowerOllamaPort)
	}

	// Override -> every resolver and the derived endpoints repoint, no literal survives.
	t.Setenv(envTailnetNetwork, "other-net")
	t.Setenv(envTailnetProxy, "edge-proxy:9050")
	t.Setenv(envTowerHost, "tower-b")
	t.Setenv(envTowerOllamaPort, "18080")

	if got := tailnetNetwork(); got != "other-net" {
		t.Errorf("tailnetNetwork() = %q, want the override other-net", got)
	}
	if got := proxyBoxName(); got != "edge-proxy" {
		t.Errorf("proxyBoxName() = %q, want the override host half edge-proxy", got)
	}
	if got, want := towerOllamaURL(), "http://tower-b:18080"; got != want {
		t.Errorf("towerOllamaURL() = %q, want %q", got, want)
	}
	if got, want := forwardListenAddr(), "127.0.0.1:18080"; got != want {
		t.Errorf("forwardListenAddr() = %q, want %q (loopback shadows the override port)", got, want)
	}
	if got, want := towerOllamaLocalURL(), "http://localhost:18080"; got != want {
		t.Errorf("towerOllamaLocalURL() = %q, want %q", got, want)
	}

	// The override flows through the exported --ts-sidecar env, including the tower
	// topology propagated for the in-container forwarder's --target default.
	p := sampleUpPlan()
	p.TSSidecar = true
	env := p.wardEnv()
	if got, want := env["WARD_TS_SOCKS5"], "socks5h://edge-proxy:9050"; got != want {
		t.Errorf("WARD_TS_SOCKS5 = %q, want %q", got, want)
	}
	if got, want := env["WARD_TOWER_OLLAMA"], "http://tower-b:18080"; got != want {
		t.Errorf("WARD_TOWER_OLLAMA = %q, want %q", got, want)
	}
	if got, want := env[envTowerHost], "tower-b"; got != want {
		t.Errorf("propagated %s = %q, want %q", envTowerHost, got, want)
	}
	if got, want := env[envTowerOllamaPort], "18080"; got != want {
		t.Errorf("propagated %s = %q, want %q", envTowerOllamaPort, got, want)
	}
	// The --network the run attaches to must follow the override too.
	if joined := strings.Join(dockerCreateArgv(p, ""), " "); !strings.Contains(joined, "--network=other-net") {
		t.Errorf("--ts-sidecar run must attach the override network other-net; got: %s", joined)
	}
}

// The buildUpPlan tailnet plumbing is covered by TestBuildUpPlanTailnet in
// container_hostnet_test.go now the two escalations are one --tailnet flag (ward#362).
