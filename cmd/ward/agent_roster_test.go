package main

import (
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// agentRosterDocPath is the committed page relative to this test's cmd/ward dir.
const agentRosterDocPath = "../../" + agentRosterDoc

// TestAgentRosterDocMatches fails when the committed docs/agent-roster.md drifts from
// the code roster's regenerated markdown - mirrors TestOpsAssetsMatchWardKDL (ward#348).
func TestAgentRosterDocMatches(t *testing.T) {
	want, err := agentRosterMarkdown()
	if err != nil {
		t.Fatalf("agentRosterMarkdown: %v", err)
	}
	got, err := os.ReadFile(agentRosterDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentRosterDocPath, err)
	}
	if want != string(got) {
		t.Errorf("%s has drifted from the code roster; regenerate it with `%s`", agentRosterDoc, agentRosterRegenHint)
	}
}

// TestAgentRosterCommandRegistered asserts `roster` mounts under the agent umbrella
// so `ward agent roster` resolves.
func TestAgentRosterCommandRegistered(t *testing.T) {
	if commandNamed(agentCommand().Commands, "roster") == nil {
		t.Fatalf("agent umbrella missing the roster command; got %v", commandNames(agentCommand().Commands))
	}
}

// TestAgentRosterEnumeratesEveryRole asserts every registered non-meta role has a
// descriptor and the four roles are all covered (ward#348).
func TestAgentRosterEnumeratesEveryRole(t *testing.T) {
	rows, err := agentRosterRows()
	if err != nil {
		t.Fatalf("agentRosterRows: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Role] = true
		if strings.TrimSpace(r.Tagline) == "" || strings.TrimSpace(r.Modes) == "" {
			t.Errorf("role %q has an empty tagline or modes column", r.Role)
		}
	}
	for _, role := range []string{"engineer", "architect", "director", "advisor"} {
		if !got[role] {
			t.Errorf("roster missing the %q role; got %v", role, rosterRoleNames(rows))
		}
	}
	// roster itself is a meta verb, never a roster entry.
	if got["roster"] {
		t.Error("roster listed itself as a role; it must be skipped as a meta command")
	}
}

// TestAgentRosterRowsRejectsUndescribedRole asserts a registered role with no
// descriptor is a hard error, not a silent omission (ward#348).
func TestAgentRosterRowsRejectsUndescribedRole(t *testing.T) {
	cmds := []*cli.Command{
		{Name: "engineer"},
		{Name: "newcomer"}, // no agentRoleInfos entry
	}
	if _, err := agentRosterRowsFrom(cmds); err == nil {
		t.Fatal("agentRosterRowsFrom accepted a role with no descriptor; want an error")
	}
}

// TestAgentRosterMarkdownShape sanity-checks the generated body: the generated-by
// header, a table per role, and a per-role doc link.
func TestAgentRosterMarkdownShape(t *testing.T) {
	md, err := agentRosterMarkdown()
	if err != nil {
		t.Fatalf("agentRosterMarkdown: %v", err)
	}
	for _, want := range []string{
		"# ward agent: the role roster",
		"ward agent roster --markdown",
		"| Role | What this specialist does | Invocation modes |",
		"[`warded engineer`](agent-engineer.md)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("generated roster body missing %q", want)
		}
	}
	if !strings.HasSuffix(md, "\n") {
		t.Error("generated roster body should end in a newline")
	}
}

func rosterRoleNames(rows []agentRosterRow) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Role)
	}
	return names
}
