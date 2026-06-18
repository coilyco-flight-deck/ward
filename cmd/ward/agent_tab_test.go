package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentTabURL covers the channel x surface matrix and the two invalid-value
// errors that guard the Warp URI fire.
func TestAgentTabURL(t *testing.T) {
	cases := []struct {
		channel, surface, want string
		wantErr                bool
	}{
		{"preview", "tab", "warppreview://tab_config/claude-agent-work", false},
		{"preview", "window", "warppreview://launch/claude-agent-work", false},
		{"stable", "tab", "warp://tab_config/claude-agent-work", false},
		{"stable", "window", "warp://launch/claude-agent-work", false},
		{"bogus", "tab", "", true},
		{"preview", "bogus", "", true},
	}
	for _, c := range cases {
		got, err := agentTabURL(c.channel, c.surface, "claude-agent-work")
		if c.wantErr {
			if err == nil {
				t.Errorf("agentTabURL(%q,%q): want error, got %q", c.channel, c.surface, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("agentTabURL(%q,%q): %v", c.channel, c.surface, err)
			continue
		}
		if got != c.want {
			t.Errorf("agentTabURL(%q,%q) = %q, want %q", c.channel, c.surface, got, c.want)
		}
	}
}

// TestWriteAgentTabQueueEntry verifies the entry round-trips as 0600 JSON under
// a freshly created queue dir, with the unix-nanos filename prefix.
func TestWriteAgentTabQueueEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	entry := agentTabQueueEntry{
		SchemaVersion: agentTabQueueSchemaVersion,
		Ref:           "coilyco-flight-deck/ward#174",
		Mode:          "claude",
		Title:         "retire dispatch",
	}
	path, err := writeAgentTabQueueEntry(dir, entry)
	if err != nil {
		t.Fatalf("writeAgentTabQueueEntry: %v", err)
	}
	if !strings.HasSuffix(path, ".json") || filepath.Dir(path) != dir {
		t.Errorf("unexpected queue path %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat queue entry: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("queue entry mode = %v, want 0600", info.Mode().Perm())
	}
	var got agentTabQueueEntry
	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read queue entry: %v", err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal queue entry: %v", err)
	}
	if got != entry {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, entry)
	}
}
