package main

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// agents_wire_test.go proves the ward#425 drain: the registry agents own their
// capability behaviour directly (no closures into core).

func testRunner() *Runner { return &Runner{Runner: &shell.Runner{Stderr: io.Discard}} }

// discardLog is the no-op Logger the ctx helpers pass where blog is not wanted.
func discardLog(string, ...any) {}

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
			rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: home, Log: discardLog, OpencodeModel: "qwen3-coder:30b", OllamaURL: "http://localhost:11434/v1"}
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
// mode resolves to its record, qwen folds to opencode, unknown falls back to claude.
func TestLookupAgentResolvesModes(t *testing.T) {
	for _, mode := range agentModes {
		if got := lookupAgent(mode).Name(); got != string(mode) {
			t.Errorf("lookupAgent(%s).Name() = %q, want %q", mode, got, mode)
		}
	}
	if got := lookupAgent(modeQwenAlias).Name(); got != "opencode" {
		t.Errorf("lookupAgent(qwen).Name() = %q, want opencode", got)
	}
	if got := lookupAgent("no-such-mode").Name(); got != "claude" {
		t.Errorf("lookupAgent(unknown).Name() = %q, want claude fallback", got)
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
		{modeCodex, ".codex.json", ".claude.json"},
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
				Mode:       string(tc.mode),
				AgentHome:  home,
				TargetName: "ward",
				QwenModel:  "qwen3-coder:30b",
				OllamaURL:  "http://localhost:11434/v1",
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
