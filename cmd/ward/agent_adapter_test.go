package main

import (
	"fmt"
	"testing"
)

// TestAgentManifestParses guards the embedded manifest: it must parse, declare
// the supported schema version, and define exactly the four modes ward ships.
func TestAgentManifestParses(t *testing.T) {
	m, err := loadAgentManifest()
	if err != nil {
		t.Fatalf("loadAgentManifest: %v", err)
	}
	if m.SchemaVersion != agentAdapterSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", m.SchemaVersion, agentAdapterSchemaVersion)
	}
	for _, mode := range agentModes {
		if _, ok := m.adapter(string(mode)); !ok {
			t.Errorf("manifest missing agent %q", mode)
		}
	}
	if len(m.Agents) != len(agentModes) {
		t.Errorf("manifest has %d agents, want %d (the agentModes set)", len(m.Agents), len(agentModes))
	}
}

// TestAgentManifestMatchesHardcodedSwitches is the ward#152 pre-req contract: the
// manifest must agree, entry for entry, with the still-live Go switches.
func TestAgentManifestMatchesHardcodedSwitches(t *testing.T) {
	m, err := loadAgentManifest()
	if err != nil {
		t.Fatalf("loadAgentManifest: %v", err)
	}
	for _, mode := range agentModes {
		a, ok := m.adapter(string(mode))
		if !ok {
			t.Errorf("manifest missing agent %q", mode)
			continue
		}
		if a.Binary != mode.agentBinary() {
			t.Errorf("%s: manifest binary %q != switch %q", mode, a.Binary, mode.agentBinary())
		}
		if a.ContextLevel != mode.contextLevel() {
			t.Errorf("%s: manifest contextLevel %d != switch %d", mode, a.ContextLevel, mode.contextLevel())
		}

		const prompt = "carry it?"
		gotArgv, gotOK := a.preflightArgv(prompt)
		wantArgv, wantOK := mode.hostPreflightArgv(prompt)
		if gotOK != wantOK {
			t.Errorf("%s: manifest preflight present=%v != switch present=%v", mode, gotOK, wantOK)
			continue
		}
		if fmt.Sprint(gotArgv) != fmt.Sprint(wantArgv) {
			t.Errorf("%s: manifest preflight argv %v != switch %v", mode, gotArgv, wantArgv)
		}
	}
}

// TestAgentManifestCodexDialect pins codex's real exec dialect in the embedded
// manifest (ward#178): headless `codex exec`, interactive `codex`, no preflight.
func TestAgentManifestCodexDialect(t *testing.T) {
	m, err := loadAgentManifest()
	if err != nil {
		t.Fatalf("loadAgentManifest: %v", err)
	}
	codex, ok := m.adapter("codex")
	if !ok {
		t.Fatal("manifest missing codex")
	}
	if got := fmt.Sprint(codex.Argv.Headless); got != fmt.Sprint([]string{"codex", "exec"}) {
		t.Errorf("codex headless argv = %v, want [codex exec]", codex.Argv.Headless)
	}
	if got := fmt.Sprint(codex.Argv.Interactive); got != fmt.Sprint([]string{"codex"}) {
		t.Errorf("codex interactive argv = %v, want [codex]", codex.Argv.Interactive)
	}
	if _, ok := codex.preflightArgv("carry it?"); ok {
		t.Error("codex must not advertise a host pre-flight one-shot yet")
	}
	// codex must not carry claude's stream-json flags anymore.
	for _, a := range codex.Argv.Headless {
		if a == "--output-format" || a == "stream-json" {
			t.Errorf("codex headless argv still borrows claude's stream-json flag %q", a)
		}
	}
}

// TestAgentManifestQwenDialect pins qwen's real opencode dialect (ward#187):
// headless [opencode run], interactive [opencode], no preflight, no stream-json.
func TestAgentManifestQwenDialect(t *testing.T) {
	m, err := loadAgentManifest()
	if err != nil {
		t.Fatalf("loadAgentManifest: %v", err)
	}
	qwen, ok := m.adapter("qwen")
	if !ok {
		t.Fatal("manifest missing qwen")
	}
	if qwen.Binary != "opencode" {
		t.Errorf("qwen binary = %q, want opencode", qwen.Binary)
	}
	if got := fmt.Sprint(qwen.Argv.Headless); got != fmt.Sprint([]string{"opencode", "run"}) {
		t.Errorf("qwen headless argv = %v, want [opencode run]", qwen.Argv.Headless)
	}
	if got := fmt.Sprint(qwen.Argv.Interactive); got != fmt.Sprint([]string{"opencode"}) {
		t.Errorf("qwen interactive argv = %v, want [opencode]", qwen.Argv.Interactive)
	}
	if _, ok := qwen.preflightArgv("carry it?"); ok {
		t.Error("qwen must not advertise a host pre-flight one-shot (ollama is in-container)")
	}
	// qwen must not borrow claude's stream-json flags: opencode prints its own
	// progress, so its stream is none.
	for _, a := range qwen.Argv.Headless {
		if a == "-p" || a == "--output-format" || a == "stream-json" {
			t.Errorf("qwen headless argv still borrows claude's stream-json flag %q", a)
		}
	}
}

// TestParseAgentManifestRejects covers the validation guards so a malformed or
// partial manifest fails loudly at load instead of driving the wrong binary.
func TestParseAgentManifestRejects(t *testing.T) {
	cases := map[string]string{
		"wrong schema":  "schemaVersion: 99\nagents:\n  - {name: x, binary: x, contextLevel: 0, argv: {headless: [x]}}\n",
		"no agents":     "schemaVersion: 1\nagents: []\n",
		"no name":       "schemaVersion: 1\nagents:\n  - {binary: x, contextLevel: 0, argv: {headless: [x]}}\n",
		"no binary":     "schemaVersion: 1\nagents:\n  - {name: x, contextLevel: 0, argv: {headless: [x]}}\n",
		"bad level":     "schemaVersion: 1\nagents:\n  - {name: x, binary: x, contextLevel: 3, argv: {headless: [x]}}\n",
		"no headless":   "schemaVersion: 1\nagents:\n  - {name: x, binary: x, contextLevel: 0, argv: {headless: []}}\n",
		"argv mismatch": "schemaVersion: 1\nagents:\n  - {name: x, binary: x, contextLevel: 0, argv: {headless: [y]}}\n",
		"duplicate": "schemaVersion: 1\nagents:\n" +
			"  - {name: x, binary: x, contextLevel: 0, argv: {headless: [x]}}\n" +
			"  - {name: x, binary: x, contextLevel: 0, argv: {headless: [x]}}\n",
	}
	for name, doc := range cases {
		if _, err := parseAgentManifest([]byte(doc)); err == nil {
			t.Errorf("%s: expected parse error, got nil", name)
		}
	}
}

// TestParseAgentManifestAccepts confirms a minimal well-formed manifest parses
// and is looked up by name.
func TestParseAgentManifestAccepts(t *testing.T) {
	doc := "schemaVersion: 1\nagents:\n  - {name: claude, binary: claude, contextLevel: 2, stream: stream-json, auth: claude-keychain, argv: {preflight: [claude, -p], headless: [claude], interactive: [claude]}}\n"
	m, err := parseAgentManifest([]byte(doc))
	if err != nil {
		t.Fatalf("parseAgentManifest: %v", err)
	}
	a, ok := m.adapter("claude")
	if !ok {
		t.Fatal("claude not found after parse")
	}
	argv, ok := a.preflightArgv("go?")
	if !ok || fmt.Sprint(argv) != fmt.Sprint([]string{"claude", "-p", "go?"}) {
		t.Errorf("preflightArgv = %v (ok=%v), want [claude -p go?]", argv, ok)
	}
}
