package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

// oneAgent builds a single-agent manifest for the validation guard cases.
func oneAgent(a agentAdapter) agentManifest {
	return agentManifest{SchemaVersion: agentAdapterSchemaVersion, Agents: []agentAdapter{a}}
}

// TestValidateAgentManifestRejects covers the validation guards; the YAML source
// is gone (ward#419), so the guards are fed the projected shape in-memory.
func TestValidateAgentManifestRejects(t *testing.T) {
	good := agentAdapter{Name: "x", Binary: "x", ContextLevel: 0, Argv: agentArgv{Headless: []string{"x"}}}
	cases := map[string]agentManifest{
		"wrong schema":  {SchemaVersion: 99, Agents: []agentAdapter{good}},
		"no agents":     {SchemaVersion: agentAdapterSchemaVersion},
		"no name":       oneAgent(agentAdapter{Binary: "x", Argv: agentArgv{Headless: []string{"x"}}}),
		"no binary":     oneAgent(agentAdapter{Name: "x", Argv: agentArgv{Headless: []string{"x"}}}),
		"bad level":     oneAgent(agentAdapter{Name: "x", Binary: "x", ContextLevel: 3, Argv: agentArgv{Headless: []string{"x"}}}),
		"no headless":   oneAgent(agentAdapter{Name: "x", Binary: "x", Argv: agentArgv{Headless: []string{}}}),
		"argv mismatch": oneAgent(agentAdapter{Name: "x", Binary: "x", Argv: agentArgv{Headless: []string{"y"}}}),
		"duplicate":     {SchemaVersion: agentAdapterSchemaVersion, Agents: []agentAdapter{good, good}},
	}
	for name, m := range cases {
		if err := validateAgentManifest(m); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

// TestValidateAgentManifestAccepts confirms a minimal well-formed manifest
// validates and is looked up by name.
func TestValidateAgentManifestAccepts(t *testing.T) {
	m := oneAgent(agentAdapter{
		Name: "claude", Binary: "claude", ContextLevel: 2, Stream: "stream-json", Auth: "claude-keychain",
		Argv: agentArgv{Preflight: []string{"claude", "-p"}, Headless: []string{"claude"}, Interactive: []string{"claude"}},
	})
	if err := validateAgentManifest(m); err != nil {
		t.Fatalf("validateAgentManifest: %v", err)
	}
	a, ok := m.adapter("claude")
	if !ok {
		t.Fatal("claude not found after validate")
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

// TestFleetMatchesGoldenFixture is the Phase 4 contract: the embedded fleet must
// match the committed golden fixture byte-for-byte after parsing.
func TestFleetMatchesGoldenFixture(t *testing.T) {
	fleet, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig: %v", err)
	}
	b, err := os.ReadFile(filepath.Join("testdata", "fleet.generated.golden.kdl"))
	if err != nil {
		t.Fatalf("read golden fleet: %v", err)
	}
	want, err := fleetconfig.Parse(b)
	if err != nil {
		t.Fatalf("parse golden fleet: %v", err)
	}
	if !reflect.DeepEqual(fleet, want) {
		t.Fatalf("embedded fleet does not match golden fixture")
	}
	if _, ok := fleetAgent(fleet, modeQwenAlias); ok {
		t.Errorf("fleet.generated.kdl carries a %q agent; %q is a back-compat alias, opencode is canonical", modeQwenAlias, modeQwenAlias)
	}
	if rt, err := parseMode(modeQwenAlias); err != nil || rt != modeOpencode {
		t.Errorf("parseMode(%q) = %q, %v; want %q (opencode canonical)", modeQwenAlias, rt, err, modeOpencode)
	}
	for _, a := range fleet.Agents {
		rt, err := parseMode(a.Name)
		if err != nil {
			t.Errorf("parseMode(%q): %v", a.Name, err)
			continue
		}
		if rt != containerMode(a.Name) {
			t.Errorf("parseMode(%q) = %q, want %q", a.Name, rt, a.Name)
		}
	}
}
