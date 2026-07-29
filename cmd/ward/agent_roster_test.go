package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuntimeConfigFixedAgentRoleDefinitions(t *testing.T) {
	defs, err := agentRoleDefinitions()
	if err != nil {
		t.Fatalf("load fixed workflow definitions: %v", err)
	}
	want := []string{roleEngineer, roleDirector, roleQA}
	if got := embeddedAgentRoleDefinitionOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("workflow order = %v, want %v", got, want)
	}
	if len(defs) != len(want) {
		t.Fatalf("workflow definitions = %d, want %d", len(defs), len(want))
	}
	for _, role := range want {
		if defs[role].Name != role {
			t.Fatalf("workflow %q definition = %#v", role, defs[role])
		}
	}
}

func TestRuntimeConfigAgentRosterDocMatchesTypedRoster(t *testing.T) {
	want, err := agentRosterMarkdown()
	if err != nil {
		t.Fatalf("render roster: %v", err)
	}
	path := filepath.Join("..", "..", agentRosterDoc)
	body, err := os.ReadFile(path) //nolint:gosec // repository test fixture.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(body) != want {
		t.Fatalf("%s drifted from typed roster. Run `%s`", agentRosterDoc, agentRosterRegenHint)
	}
}
