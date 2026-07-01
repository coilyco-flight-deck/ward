package main

import (
	"fmt"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
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

// TestAgentManifestOpencodeDialect pins opencode's real dialect (ward#187; roster
// key renamed from qwen by ward#401): headless [opencode run], no stream-json.
func TestAgentManifestOpencodeDialect(t *testing.T) {
	m, err := loadAgentManifest()
	if err != nil {
		t.Fatalf("loadAgentManifest: %v", err)
	}
	opencode, ok := m.adapter("opencode")
	if !ok {
		t.Fatal("manifest missing opencode")
	}
	if opencode.Binary != "opencode" {
		t.Errorf("opencode binary = %q, want opencode", opencode.Binary)
	}
	if got := fmt.Sprint(opencode.Argv.Headless); got != fmt.Sprint([]string{"opencode", "run"}) {
		t.Errorf("opencode headless argv = %v, want [opencode run]", opencode.Argv.Headless)
	}
	if got := fmt.Sprint(opencode.Argv.Interactive); got != fmt.Sprint([]string{"opencode"}) {
		t.Errorf("opencode interactive argv = %v, want [opencode]", opencode.Argv.Interactive)
	}
	if _, ok := opencode.preflightArgv("carry it?"); ok {
		t.Error("opencode must not advertise a host pre-flight one-shot (ollama is in-container)")
	}
	// opencode must not borrow claude's stream-json flags: it prints its own
	// progress, so its stream is none.
	for _, a := range opencode.Argv.Headless {
		if a == "-p" || a == "--output-format" || a == "stream-json" {
			t.Errorf("opencode headless argv still borrows claude's stream-json flag %q", a)
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

// fleetAgent looks a parsed fleet agent up by name (test helper).
func fleetAgent(f fleetconfig.Fleet, name string) (fleetconfig.Agent, bool) {
	for _, a := range f.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return fleetconfig.Agent{}, false
}

// TestFleetManifestSwitchesThreeWayPin is the ward#415 three-way contract:
// fleet.generated.kdl, agent-adapters.yaml, and the parseMode roster must agree.
func TestFleetManifestSwitchesThreeWayPin(t *testing.T) {
	fleet, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig: %v", err)
	}
	manifest, err := loadAgentManifest()
	if err != nil {
		t.Fatalf("loadAgentManifest: %v", err)
	}

	// Roster sizes must match the canonical parseMode set exactly, so an agent
	// added to one source but not the others is caught, not silently tolerated.
	if len(fleet.Agents) != len(agentModes) {
		t.Errorf("fleet has %d agents, want %d (the agentModes roster)", len(fleet.Agents), len(agentModes))
	}
	if len(manifest.Agents) != len(agentModes) {
		t.Errorf("manifest has %d agents, want %d (the agentModes roster)", len(manifest.Agents), len(agentModes))
	}

	for _, mode := range agentModes {
		name := string(mode)
		fa, ok := fleetAgent(fleet, name)
		if !ok {
			t.Errorf("fleet.generated.kdl is missing agent %q (in the parseMode roster)", name)
			continue
		}
		ad, ok := manifest.adapter(name)
		if !ok {
			t.Errorf("agent-adapters.yaml is missing agent %q (in the parseMode roster)", name)
			continue
		}

		// The parseMode roster is the third anchor: its name must round-trip, and
		// its own switches (binary, context level) must agree with both sources.
		rt, err := parseMode(name)
		if err != nil || rt != mode {
			t.Errorf("parseMode(%q) = %q, %v; want %q", name, rt, err, mode)
		}
		if fa.Binary != mode.agentBinary() || ad.Binary != mode.agentBinary() {
			t.Errorf("%s binary: fleet %q / manifest %q / switch %q disagree", name, fa.Binary, ad.Binary, mode.agentBinary())
		}
		if fa.ContextLevel != mode.contextLevel() || ad.ContextLevel != mode.contextLevel() {
			t.Errorf("%s contextLevel: fleet %d / manifest %d / switch %d disagree", name, fa.ContextLevel, ad.ContextLevel, mode.contextLevel())
		}

		// Stream + auth are fleet<->manifest fields (no Go switch); pin them so the
		// two denormalized copies cannot drift apart.
		if fa.Stream != ad.Stream {
			t.Errorf("%s stream: fleet %q != manifest %q", name, fa.Stream, ad.Stream)
		}
		if fa.Auth != ad.Auth {
			t.Errorf("%s auth: fleet %q != manifest %q", name, fa.Auth, ad.Auth)
		}

		// argv (all three modes) must agree token-for-token across fleet + manifest.
		if got, want := strings.Join(fa.Argv.Preflight, " "), strings.Join(ad.Argv.Preflight, " "); got != want {
			t.Errorf("%s preflight argv: fleet %q != manifest %q", name, got, want)
		}
		if got, want := strings.Join(fa.Argv.Headless, " "), strings.Join(ad.Argv.Headless, " "); got != want {
			t.Errorf("%s headless argv: fleet %q != manifest %q", name, got, want)
		}
		if got, want := strings.Join(fa.Argv.Interactive, " "), strings.Join(ad.Argv.Interactive, " "); got != want {
			t.Errorf("%s interactive argv: fleet %q != manifest %q", name, got, want)
		}
	}

	// The qwen back-compat alias resolves to the canonical opencode roster key, and
	// no source carries a literal `qwen` agent (opencode is canonical post-#412).
	if rt, err := parseMode(modeQwenAlias); err != nil || rt != modeOpencode {
		t.Errorf("parseMode(%q) = %q, %v; want %q (opencode canonical)", modeQwenAlias, rt, err, modeOpencode)
	}
	if _, ok := fleetAgent(fleet, modeQwenAlias); ok {
		t.Errorf("fleet.generated.kdl carries a %q agent; %q is a back-compat alias, opencode is canonical", modeQwenAlias, modeQwenAlias)
	}
	if _, ok := manifest.adapter(modeQwenAlias); ok {
		t.Errorf("agent-adapters.yaml carries a %q agent; %q is a back-compat alias, opencode is canonical", modeQwenAlias, modeQwenAlias)
	}
}
