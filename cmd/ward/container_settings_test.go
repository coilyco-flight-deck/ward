package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContainerSettingsPolicy locks the container permission policy: valid JSON,
// bypassPermissions, and the minimal force-push/history-rewrite deny wall.
func TestContainerSettingsPolicy(t *testing.T) {
	data, err := containerAssets.ReadFile("containerassets/settings.container.json")
	if err != nil {
		t.Fatalf("read embedded settings: %v", err)
	}
	var s struct {
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
	joined := strings.Join(s.Permissions.Deny, " ")
	for _, want := range []string{"git push --force", "git push -f", "git reset --hard", "git clean -fd"} {
		if !strings.Contains(joined, want) {
			t.Errorf("deny wall missing %q; got %v", want, s.Permissions.Deny)
		}
	}
}
