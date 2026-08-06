package main

import (
	"os"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agents"
)

// TestAgentHarnessContractCoversRegistry derives the shipped harness inventory
// from the registry and keeps the consolidated compatibility contract honest.
func TestAgentHarnessContractCoversRegistry(t *testing.T) {
	const path = "../../docs/agent-harnesses.md"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := strings.ToLower(string(raw))
	for name := range agents.Registry() {
		if !strings.Contains(doc, name) {
			t.Errorf("registered harness %q is absent from %s", name, path)
		}
	}
}
