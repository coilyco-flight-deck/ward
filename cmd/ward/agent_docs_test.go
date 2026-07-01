package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agents"
)

// TestAgentDocsCoverRegistry fails when a registered harness has no matching
// docs/agent-<name>.md page. It mirrors the self-curing roster drift pattern.
func TestAgentDocsCoverRegistry(t *testing.T) {
	for name := range agents.Registry() {
		path := filepath.Clean(filepath.Join("..", "..", "docs", "agent-"+name+".md"))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("registered agent %q is missing %s", name, path)
		}
	}
}
