package goose

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func noopLog(string, ...any) {}

// TestConfigYAML omits OLLAMA_HOST when no host resolved, includes it otherwise.
func TestConfigYAML(t *testing.T) {
	noHost := configYAML("ollama", "qwen3-coder:30b", "")
	if strings.Contains(noHost, "OLLAMA_HOST") {
		t.Errorf("no-host config should omit OLLAMA_HOST:\n%s", noHost)
	}
	if !strings.Contains(noHost, "GOOSE_PROVIDER: ollama") || !strings.Contains(noHost, "GOOSE_MODEL: qwen3-coder:30b") {
		t.Errorf("missing provider/model:\n%s", noHost)
	}
	withHost := configYAML("ollama", "qwen3-coder:30b", "http://tower:11434")
	if !strings.Contains(withHost, "OLLAMA_HOST: http://tower:11434") {
		t.Errorf("with-host config should include OLLAMA_HOST:\n%s", withHost)
	}
}

// TestComposeConfigScrubsEnv: ComposeConfig writes config.yaml then scrubs the
// bootstrap-only WARD_GOOSE_OLLAMA_HOST_B64 env var, so it can't leak (ward#357).
func TestComposeConfigScrubsEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv(ollamaHostEnvKey, base64.StdEncoding.EncodeToString([]byte("http://tower:11434")))
	rc := agentsapi.RunCtx{AgentHome: home, Log: noopLog}
	if err := (Agent{}).ComposeConfig(rc); err != nil {
		t.Fatalf("ComposeConfig: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".config", "goose", "config.yaml"))
	if err != nil {
		t.Fatalf("expected config.yaml written: %v", err)
	}
	if !strings.Contains(string(got), "OLLAMA_HOST: http://tower:11434") {
		t.Errorf("config.yaml missing resolved host:\n%s", got)
	}
	if v := os.Getenv(ollamaHostEnvKey); v != "" {
		t.Errorf("%s should be scrubbed after seeding, got %q", ollamaHostEnvKey, v)
	}
}

// TestComposeConfigUsesRunCtxModelVerbatim pins the Goose model pass-through:
// the run context model lands in config.yaml unchanged, without normalization.
func TestComposeConfigUsesRunCtxModelVerbatim(t *testing.T) {
	home := t.TempDir()
	model := "qwen3-coder:30b-instruct"
	rc := agentsapi.RunCtx{AgentHome: home, Log: noopLog, GooseModel: model}
	if err := (Agent{}).ComposeConfig(rc); err != nil {
		t.Fatalf("ComposeConfig: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".config", "goose", "config.yaml"))
	if err != nil {
		t.Fatalf("expected config.yaml written: %v", err)
	}
	if !strings.Contains(string(got), "GOOSE_MODEL: "+model) {
		t.Fatalf("config.yaml missing verbatim model %q:\n%s", model, got)
	}
	if strings.Contains(string(got), "GOOSE_MODEL: qwen3-coder:30b\n") {
		t.Fatalf("config.yaml should not normalize the model:\n%s", got)
	}
}
