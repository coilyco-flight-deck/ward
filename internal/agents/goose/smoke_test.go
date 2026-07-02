package goose

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// TestConfiguredOllamaHost reads the OLLAMA_HOST line from the written config,
// falling back to goose's built-in default when the file or line is absent.
func TestConfiguredOllamaHost(t *testing.T) {
	// No config file yet -> the built-in default.
	home := t.TempDir()
	if got := configuredOllamaHost(agentsapi.RunCtx{AgentHome: home}); got != defaultGooseOllamaHost {
		t.Errorf("missing config: got %q, want default %q", got, defaultGooseOllamaHost)
	}
	// A seeded host is read back verbatim.
	dir := filepath.Join(home, ".config", "goose")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "GOOSE_PROVIDER: ollama\nGOOSE_MODEL: qwen3-coder:30b\nOLLAMA_HOST: http://tower:11434\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := configuredOllamaHost(agentsapi.RunCtx{AgentHome: home}); got != "http://tower:11434" {
		t.Errorf("seeded host: got %q, want %q", got, "http://tower:11434")
	}
}

// TestPreLaunchCheckReachable proves goose gates on the configured endpoint: a
// headless run against a live listener passes.
func TestPreLaunchCheckReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "goose")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "GOOSE_PROVIDER: ollama\nOLLAMA_HOST: http://" + ln.Addr().String() + "\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: home, Headless: true, Log: noopLog}
	if err := (Agent{}).PreLaunchCheck(rc); err != nil {
		t.Errorf("PreLaunchCheck against a live endpoint should pass, got %v", err)
	}
	// A non-headless run never probes.
	rc.Headless = false
	if err := (Agent{}).PreLaunchCheck(rc); err != nil {
		t.Errorf("non-headless PreLaunchCheck should no-op, got %v", err)
	}
}
