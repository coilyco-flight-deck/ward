package main

import (
	"encoding/json"
	"testing"
)

// TestContainerSettingsPolicy locks the container permission policy: valid JSON,
// bypassPermissions, and no deny wall - isolation is the sole boundary (ward#375).
func TestContainerSettingsPolicy(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/settings.container.json")
	if err != nil {
		t.Fatalf("read embedded settings: %v", err)
	}
	var s struct {
		TUI         string `json:"tui"`
		Permissions struct {
			DefaultMode string   `json:"defaultMode"`
			Deny        []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings.container.json is not valid JSON: %v", err)
	}
	if s.Permissions.DefaultMode != "bypassPermissions" {
		t.Errorf("defaultMode = %q, want bypassPermissions", s.Permissions.DefaultMode)
	}
	// Fresh containers default to the fullscreen flicker-free renderer (ward#317).
	if s.TUI != "fullscreen" {
		t.Errorf("tui = %q, want fullscreen", s.TUI)
	}
	// The deny wall is gone: container isolation is the sole boundary (ward#375).
	if len(s.Permissions.Deny) != 0 {
		t.Errorf("deny wall should be empty; got %v", s.Permissions.Deny)
	}
}
