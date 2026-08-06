package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestContainerSettingsPolicy locks valid generated JSON, bypassPermissions,
// and no deny wall - isolation is the sole boundary (ward#375).
func TestContainerSettingsPolicy(t *testing.T) {
	home := t.TempDir()
	r := testRunner()
	rc := r.agentRunCtx(context.Background(), bootstrapEnv{Mode: string(modeClaude), AgentHome: home}, nil)
	composeAgentContainer(lookupAgent(modeClaude), rc)
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read composed container settings: %v", err)
	}
	var s struct {
		TUI              string           `json:"tui"`
		DeniedMCPServers []map[string]any `json:"deniedMcpServers"`
		Permissions      struct {
			DefaultMode string   `json:"defaultMode"`
			Deny        []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("generated container settings are not valid JSON: %v", err)
	}
	if s.Permissions.DefaultMode != "bypassPermissions" {
		t.Errorf("defaultMode = %q, want bypassPermissions", s.Permissions.DefaultMode)
	}
	// Fresh containers default to the fullscreen flicker-free renderer (ward#317).
	if s.TUI != "fullscreen" {
		t.Errorf("tui = %q, want fullscreen", s.TUI)
	}
	if len(s.DeniedMCPServers) != 1 || s.DeniedMCPServers[0]["serverName"] != "claude-in-chrome" {
		t.Errorf("deniedMcpServers = %v, want claude-in-chrome", s.DeniedMCPServers)
	}
	// The deny wall is gone: container isolation is the sole boundary (ward#375).
	if len(s.Permissions.Deny) != 0 {
		t.Errorf("deny wall should be empty; got %v", s.Permissions.Deny)
	}
}
