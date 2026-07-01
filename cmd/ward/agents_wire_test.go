package main

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentspi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// agents_wire_test.go proves the ward#412 delegation: a wired agent's capability
// methods route to the live cmd/ward funcs; DATA-only registry agents no-op.

func testRunner() *Runner { return &Runner{Runner: &shell.Runner{Stderr: io.Discard}} }

// TestWiredClaudeWriteCredsDelegates confirms claude's wired WriteCreds runs the
// live writeClaudeCreds: the base64 blob lands as ~/.claude/.credentials.json.
func TestWiredClaudeWriteCredsDelegates(t *testing.T) {
	home := t.TempDir()
	const secret = `{"claudeAiOauth":{"accessToken":"tok"}}`
	t.Setenv("WARD_CLAUDE_CREDS_B64", base64.StdEncoding.EncodeToString([]byte(secret)))

	r := testRunner()
	a, ok := r.wireAgent(modeClaude)
	if !ok {
		t.Fatal("wireAgent(modeClaude) not ok")
	}
	cp, ok := a.(agentspi.CredentialProvider)
	if !ok {
		t.Fatal("wired claude must be a CredentialProvider")
	}
	rc := agentspi.RunCtx{Ctx: context.Background(), AgentHome: home}
	if err := cp.WriteCreds(rc); err != nil {
		t.Fatalf("WriteCreds: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if err != nil {
		t.Fatalf("delegated writeClaudeCreds did not write the cred file: %v", err)
	}
	if string(got) != secret {
		t.Errorf("cred file = %q, want %q", got, secret)
	}
}

// TestWiredConfigComposersDelegate confirms each config-composer agent's wired
// ComposeConfig runs the matching live compose func (side effect: the file lands).
func TestWiredConfigComposersDelegate(t *testing.T) {
	cases := []struct {
		mode containerMode
		rel  string // config path relative to AgentHome the live func writes
	}{
		{modeCodex, filepath.Join(".codex", "config.toml")},
		{modeOpencode, filepath.Join(".config", "opencode", "opencode.json")},
		{modeGoose, filepath.Join(".config", "goose", "config.yaml")},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			home := t.TempDir()
			r := testRunner()
			a, ok := r.wireAgent(tc.mode)
			if !ok {
				t.Fatalf("wireAgent(%s) not ok", tc.mode)
			}
			cc, ok := a.(agentspi.ConfigComposer)
			if !ok {
				t.Fatalf("wired %s must be a ConfigComposer", tc.mode)
			}
			rc := agentspi.RunCtx{Ctx: context.Background(), AgentHome: home, OpencodeModel: "qwen3-coder:30b", OllamaURL: "http://localhost:11434/v1"}
			if err := cc.ComposeConfig(rc); err != nil {
				t.Fatalf("ComposeConfig: %v", err)
			}
			if _, err := os.Stat(filepath.Join(home, tc.rel)); err != nil {
				t.Errorf("delegated compose for %s did not write %s: %v", tc.mode, tc.rel, err)
			}
		})
	}
}

// TestRegistryAgentsNoOpUnwired confirms the DATA-only registry agents forward to
// nil closures safely (no panic, no filesystem write) - Phase 2 flips no call site.
func TestRegistryAgentsNoOpUnwired(t *testing.T) {
	home := t.TempDir()
	rc := agentspi.RunCtx{Ctx: context.Background(), AgentHome: home}
	for _, mode := range agentModes {
		a, _ := agents.Lookup(string(mode))
		if cp, ok := a.(agentspi.CredentialProvider); ok {
			if err := cp.WriteCreds(rc); err != nil {
				t.Errorf("%s: unwired WriteCreds returned %v, want nil no-op", mode, err)
			}
			if lines := cp.ResolveCreds(agentspi.HostCtx{Ctx: context.Background()}); lines != nil {
				t.Errorf("%s: unwired ResolveCreds returned %v, want nil", mode, lines)
			}
		}
		if cc, ok := a.(agentspi.ConfigComposer); ok {
			if err := cc.ComposeConfig(rc); err != nil {
				t.Errorf("%s: unwired ComposeConfig returned %v, want nil no-op", mode, err)
			}
		}
	}
	// Nothing should have been written by the unwired no-ops.
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("unwired agents wrote %d entries under HOME, want 0", len(entries))
	}
}
