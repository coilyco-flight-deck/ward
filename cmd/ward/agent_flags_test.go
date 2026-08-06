package main

import (
	"os"
	"strings"
	"testing"
)

// agentFlagsDocPath is the committed page relative to this test's cmd/ward dir.
const agentFlagsDocPath = "../../" + agentFlagsDoc

// TestAgentFlagsDocMatches fails when the committed docs/agent-flags.md drifts from
// the code flag tree's regenerated markdown.
func TestAgentFlagsDocMatches(t *testing.T) {
	want, err := agentFlagsMarkdown()
	if err != nil {
		t.Fatalf("agentFlagsMarkdown: %v", err)
	}
	got, err := os.ReadFile(agentFlagsDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentFlagsDocPath, err)
	}
	if want != string(got) {
		t.Errorf("%s has drifted from the code flag tree; regenerate it with `%s`", agentFlagsDoc, agentFlagsRegenHint)
	}
}

// TestAgentFlagsCommandRegistered asserts `flags` mounts under the agent umbrella
// so `ward agent flags` resolves.
func TestAgentFlagsCommandRegistered(t *testing.T) {
	if commandNamed(agentCommand().Commands, "flags") == nil {
		t.Fatalf("agent umbrella missing the flags command; got %v", commandNames(agentCommand().Commands))
	}
}

// TestAgentFlagsMarkdownShape sanity-checks the generated body: the doc_goal
// front-matter, the generated-by header, and a few command sections.
func TestAgentFlagsMarkdownShape(t *testing.T) {
	md, err := agentFlagsMarkdown()
	if err != nil {
		t.Fatalf("agentFlagsMarkdown: %v", err)
	}
	for _, want := range []string{
		"---\ndoc_goal: ",
		"# ward agent: the flag tree",
		"ward agent flags --markdown",
		"## `ward agent`",
		"## `ward agent engineer`",
		"## `ward agent pr runs`",
		"(hidden)",
		"--workflow",
		"--instructions-file",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("generated flag tree missing %q", want)
		}
	}
	if !strings.HasSuffix(md, "\n") {
		t.Error("generated flag tree should end in a newline")
	}
}
