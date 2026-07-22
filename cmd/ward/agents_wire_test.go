package main

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"
	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// agents_wire_test.go proves the ward#425 drain: the registry agents own their
// capability behaviour directly (no closures into core).

func testRunner() *Runner { return &Runner{Runner: &shell.Runner{Stderr: io.Discard}} }

// discardLog is the no-op Logger the ctx helpers pass where blog is not wanted.
func discardLog(string, ...any) {}

type panicExec struct{}

func (panicExec) Exec(context.Context, string, ...string) error { panic("unexpected Exec call") }

func (panicExec) Capture(context.Context, string, ...string) ([]byte, error) {
	panic("unexpected Capture call")
}

// TestRegistryClaudeWriteCreds confirms claude's registry agent writes the injected
// base64 blob to ~/.claude/.credentials.json (behaviour lives in the folder, ward#425).
func TestRegistryClaudeWriteCreds(t *testing.T) {
	home := t.TempDir()
	const secret = `{"claudeAiOauth":{"accessToken":"tok"}}`
	t.Setenv("WARD_CLAUDE_CREDS_B64", base64.StdEncoding.EncodeToString([]byte(secret)))

	a, ok := agents.Lookup(string(modeClaude))
	if !ok {
		t.Fatal("Lookup(claude) not ok")
	}
	cp, ok := a.(agentsapi.CredentialProvider)
	if !ok {
		t.Fatal("claude must be a CredentialProvider")
	}
	rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: home, Log: discardLog}
	if err := cp.WriteCreds(rc); err != nil {
		t.Fatalf("WriteCreds: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if err != nil {
		t.Fatalf("WriteCreds did not write the cred file: %v", err)
	}
	if string(got) != secret {
		t.Errorf("cred file = %q, want %q", got, secret)
	}
}

// TestRegistryConfigComposersWrite confirms each config-composer registry agent's
// ComposeConfig writes its file (behaviour lives in the folders now, ward#425).
func TestRegistryConfigComposersWrite(t *testing.T) {
	cases := []struct {
		mode containerMode
		rel  string // config path relative to AgentHome the folder func writes
	}{
		{modeCodex, filepath.Join(".codex", "config.toml")},
		{modeOpencode, filepath.Join(".config", "opencode", "opencode.json")},
		{modeGoose, filepath.Join(".config", "goose", "config.yaml")},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			home := t.TempDir()
			a, ok := agents.Lookup(string(tc.mode))
			if !ok {
				t.Fatalf("Lookup(%s) not ok", tc.mode)
			}
			cc, ok := a.(agentsapi.ConfigComposer)
			if !ok {
				t.Fatalf("%s must be a ConfigComposer", tc.mode)
			}
			rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: home, Log: discardLog, OpencodeModel: "qwen3-coder:30b", GooseModel: "configured-model", OllamaURL: "http://host.docker.internal:8082/v1"}
			if err := cc.ComposeConfig(rc); err != nil {
				t.Fatalf("ComposeConfig: %v", err)
			}
			if _, err := os.Stat(filepath.Join(home, tc.rel)); err != nil {
				t.Errorf("ComposeConfig for %s did not write %s: %v", tc.mode, tc.rel, err)
			}
		})
	}
}

// TestCredWritesSafeWithoutInjection confirms WriteCreds no-ops without a *_B64
// blob and ResolveCreds yields nil when the host source is missing (no panic).
func TestCredWritesSafeWithoutInjection(t *testing.T) {
	for _, mode := range agentModes {
		a, _ := agents.Lookup(string(mode))
		cp, ok := a.(agentsapi.CredentialProvider)
		if !ok {
			continue
		}
		home := t.TempDir()
		rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: home, Log: discardLog}
		if err := cp.WriteCreds(rc); err != nil {
			t.Errorf("%s: WriteCreds no-inject returned %v", mode, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".claude", ".credentials.json")); err == nil {
			t.Errorf("%s: WriteCreds wrote a claude cred file with nothing injected", mode)
		}
		hc := agentsapi.HostCtx{Ctx: context.Background(), GOOS: "linux", Home: t.TempDir(), Exec: &shell.Runner{Stderr: io.Discard}, Log: discardLog}
		if lines := cp.ResolveCreds(hc); lines != nil {
			t.Errorf("%s: ResolveCreds with missing source = %v, want nil", mode, lines)
		}
	}
}

// TestLookupAgentResolvesModes pins the Phase 3 (ward#418) data-read surface: every
// mode resolves to its record, unknown falls back to claude.
func TestLookupAgentResolvesModes(t *testing.T) {
	for _, mode := range agentModes {
		if got := lookupAgent(mode).Name(); got != string(mode) {
			t.Errorf("lookupAgent(%s).Name() = %q, want %q", mode, got, mode)
		}
	}
	if got := lookupAgent("no-such-mode").Name(); got != "claude" {
		t.Errorf("lookupAgent(unknown).Name() = %q, want claude fallback", got)
	}
}

// TestComposeAgentContainerCodexSurfaceTrust is the ward#678 director regression: a
// read-only codex surface, wired as runContainerBootstrap wires it, trusts its cwd.
func TestComposeAgentContainerCodexSurfaceTrust(t *testing.T) {
	home := t.TempDir()
	r := testRunner()
	e := bootstrapEnv{
		Mode:       string(modeCodex),
		AgentHome:  home,
		TargetName: "agentic-os",
		ReadOnly:   true, // the director surface container (WARD_READONLY=1)
	}
	rc := r.agentRunCtx(context.Background(), e, nil)
	rc.TrustDirs = agentTrustDirs(e)
	composeAgentContainer(lookupAgent(modeCodex), rc)
	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("surface compose did not write ~/.codex/config.toml: %v", err)
	}
	for _, dir := range []string{"/workspace/agentic-os", "/workspace"} {
		want := "[projects.\"" + dir + "\"]\ntrust_level = \"trusted\"\n"
		if !strings.Contains(string(data), want) {
			t.Errorf("surface config.toml missing trust table for %s\n---\n%s", dir, data)
		}
	}
}

// TestComposeAgentContainerPerMode confirms the dispatch helper runs only each
// mode's capabilities (claude onboarding, codex/opencode/goose config), no bleed.
func TestComposeAgentContainerPerMode(t *testing.T) {
	cases := []struct {
		mode    containerMode
		present string // a path (relative to HOME) the mode's setup must write
		absent  string // a path a different mode would write, proving no bleed
	}{
		{modeClaude, ".claude.json", filepath.Join(".codex", "config.toml")},
		{modeCodex, filepath.Join(".codex", "config.toml"), ".claude.json"},
		{modeOpencode, filepath.Join(".config", "opencode", "opencode.json"), filepath.Join(".codex", "config.toml")},
		{modeGoose, filepath.Join(".config", "goose", "config.yaml"), filepath.Join(".codex", "config.toml")},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			home := t.TempDir()
			r := testRunner()
			a := lookupAgent(tc.mode)
			// TargetName drives claude's onboarding project entry; the model/url feed
			// the opencode composer.
			rc := r.agentRunCtx(context.Background(), bootstrapEnv{
				Mode:          string(tc.mode),
				AgentHome:     home,
				TargetName:    "ward",
				OpencodeModel: "qwen3-coder:30b",
				GooseModel:    "configured-model",
				OllamaURL:     "http://host.docker.internal:8082/v1",
			}, nil)
			composeAgentContainer(a, rc)
			if _, err := os.Stat(filepath.Join(home, tc.present)); err != nil {
				t.Errorf("%s: composeAgentContainer did not write %s: %v", tc.mode, tc.present, err)
			}
			if _, err := os.Stat(filepath.Join(home, tc.absent)); err == nil {
				t.Errorf("%s: composeAgentContainer wrote %s from another mode", tc.mode, tc.absent)
			}
		})
	}
}

// TestComposeAgentContainerGooseUsesRunCtxModel pins the end-to-end Goose path:
// the resolved run context model lands in goose's config.yaml unchanged.
func TestComposeAgentContainerGooseUsesRunCtxModel(t *testing.T) {
	home := t.TempDir()
	r := testRunner()
	rc := r.agentRunCtx(context.Background(), bootstrapEnv{
		Mode:       string(modeGoose),
		AgentHome:  home,
		TargetName: "ward",
		GooseModel: "qwen3-coder:30b-instruct",
		OllamaURL:  "http://host.docker.internal:8082/v1",
	}, nil)
	composeAgentContainer(lookupAgent(modeGoose), rc)
	got, err := os.ReadFile(filepath.Join(home, ".config", "goose", "config.yaml"))
	if err != nil {
		t.Fatalf("read goose config: %v", err)
	}
	if !strings.Contains(string(got), "GOOSE_MODEL: qwen3-coder:30b-instruct") {
		t.Fatalf("goose config missing verbatim model:\n%s", got)
	}
}

// TestSelfContainedHarnessInstall verifies the self-contained harnesses
// explicitly prove their binary is already present and do not need Exec.
func TestSelfContainedHarnessInstall(t *testing.T) {
	cases := []containerMode{modeClaude, modeCodex, modeGoose}
	for _, mode := range cases {
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			bin := lookupAgent(mode).Record().Binary
			if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatalf("write stub %s: %v", bin, err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: t.TempDir(), Log: discardLog, Exec: panicExec{}}
			if err := lookupAgent(mode).Install(rc); err != nil {
				t.Fatalf("%s Install: %v", mode, err)
			}
		})
	}
}

type installHarnessProbeAgent struct {
	binary        string
	installDir    string
	installCalled bool
}

func (a *installHarnessProbeAgent) Name() string { return "probe" }

func (a *installHarnessProbeAgent) Record() agentsapi.Manifest {
	return agentsapi.Manifest{Binary: a.binary}
}

func (a *installHarnessProbeAgent) Signer() attribution.Signer { return attribution.Signer{} }

func (a *installHarnessProbeAgent) Install(_ agentsapi.RunCtx) error {
	a.installCalled = true
	return os.WriteFile(filepath.Join(a.installDir, a.binary), []byte("#!/bin/sh\nexit 0\n"), 0o755)
}

func (a *installHarnessProbeAgent) LaunchArgv(agentsapi.RunCtx) ([]string, bool) {
	return []string{a.binary}, false
}

func (a *installHarnessProbeAgent) PreflightArgv(string) ([]string, bool) { return nil, false }

// TestInstallHarnessRunsBeforeBinaryCheck proves the bootstrap helper calls the
// required install hook before it validates the harness binary.
func TestInstallHarnessRunsBeforeBinaryCheck(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	agent := &installHarnessProbeAgent{binary: "probe-harness", installDir: binDir}
	if err := installHarness(agent, agentsapi.RunCtx{Ctx: context.Background(), AgentHome: t.TempDir(), Log: discardLog}); err != nil {
		t.Fatalf("installHarness: %v", err)
	}
	if !agent.installCalled {
		t.Fatal("installHarness did not call Install before verifying the binary")
	}
	if _, err := os.Stat(filepath.Join(binDir, agent.binary)); err != nil {
		t.Fatalf("Install did not stage the harness binary: %v", err)
	}
}
