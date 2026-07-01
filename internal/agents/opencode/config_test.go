package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func noopLog(string, ...any) {}

// TestConfigJSON keeps the literal $schema key (not interpolated) and
// interpolates the model + URL in the right places.
func TestConfigJSON(t *testing.T) {
	got := configJSON("qwen3-coder:30b", "http://localhost:11434/v1")
	for _, want := range []string{
		`"$schema": "https://opencode.ai/config.json"`,
		`"model": "ollama/qwen3-coder:30b"`,
		`"baseURL": "http://localhost:11434/v1"`,
		`"qwen3-coder:30b": {}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("opencode config missing %q in:\n%s", want, got)
		}
	}
}

// TestComposeConfigWrites confirms ComposeConfig writes the config under
// ~/.config/opencode from the RunCtx model/url knobs.
func TestComposeConfigWrites(t *testing.T) {
	home := t.TempDir()
	rc := agentsapi.RunCtx{AgentHome: home, Log: noopLog, OpencodeModel: "qwen3-coder:30b", OllamaURL: "http://localhost:11434/v1"}
	if err := (Agent{}).ComposeConfig(rc); err != nil {
		t.Fatalf("ComposeConfig: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("expected opencode.json written: %v", err)
	}
	if !strings.Contains(string(got), `"model": "ollama/qwen3-coder:30b"`) {
		t.Errorf("opencode.json missing model:\n%s", got)
	}
}
