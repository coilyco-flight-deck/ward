package main

import (
	"fmt"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentspi"
)

// runCtxForPosture builds the narrow RunCtx the registry's LaunchArgv reads for a
// given launch posture, so the contract test can compare it to buildAgentArgv.
func runCtxForPosture(seed []string, headless, ask bool) agentspi.RunCtx {
	return agentspi.RunCtx{Seed: seed, Headless: headless, Ask: ask}
}

// agents_registry_contract_test.go is the ward#412 generalized contract: the
// registry must agree, entry for entry, with the live cmd/ward switches (ward#152).

// TestRegistryCoversEveryMode pins the registry roster to agentModes: exactly the
// modes ward drives, keyed by their roster name, no more and no fewer.
func TestRegistryCoversEveryMode(t *testing.T) {
	reg := agents.Registry()
	if len(reg) != len(agentModes) {
		t.Errorf("registry has %d agents, want %d (the agentModes set)", len(reg), len(agentModes))
	}
	for _, mode := range agentModes {
		a, ok := reg[string(mode)]
		if !ok {
			t.Errorf("registry missing agent %q", mode)
			continue
		}
		if a.Name() != string(mode) {
			t.Errorf("registry[%q].Name() = %q, want %q", mode, a.Name(), mode)
		}
	}
}

// TestRegistryMatchesHardcodedSwitches is the core Phase 2 contract: for every
// mode, the registry agent's data + argv must equal the live Go switches.
func TestRegistryMatchesHardcodedSwitches(t *testing.T) {
	const prompt = "carry it?"
	seed := []string{"work issue #5"}

	for _, mode := range agentModes {
		a, ok := agents.Lookup(string(mode))
		if !ok {
			t.Errorf("registry Lookup(%q) missing", mode)
			continue
		}
		rec := a.Record()

		if a.Name() != string(mode) {
			t.Errorf("%s: registry Name %q != mode %q", mode, a.Name(), mode)
		}
		if rec.Binary != mode.agentBinary() {
			t.Errorf("%s: registry binary %q != switch %q", mode, rec.Binary, mode.agentBinary())
		}
		if rec.ContextLevel != mode.contextLevel() {
			t.Errorf("%s: registry contextLevel %d != switch %d", mode, rec.ContextLevel, mode.contextLevel())
		}

		// Signer: identity + marker + via + email must match agentSigner exactly.
		if got, want := a.Signer(), mode.agentSigner(); got != want {
			t.Errorf("%s: registry Signer %+v != switch %+v", mode, got, want)
		}

		// PreflightArgv must match hostPreflightArgv (present-or-not, and argv).
		gotArgv, gotOK := a.PreflightArgv(prompt)
		wantArgv, wantOK := mode.hostPreflightArgv(prompt)
		if gotOK != wantOK {
			t.Errorf("%s: registry preflight present=%v != switch present=%v", mode, gotOK, wantOK)
		} else if fmt.Sprint(gotArgv) != fmt.Sprint(wantArgv) {
			t.Errorf("%s: registry preflight argv %v != switch %v", mode, gotArgv, wantArgv)
		}

		// LaunchArgv must match buildAgentArgv across every launch posture.
		for _, posture := range []struct {
			name          string
			headless, ask bool
		}{
			{"interactive", false, false},
			{"headless", true, false},
			{"ask", false, true},
		} {
			e := bootstrapEnv{Mode: string(mode), Agent: mode.agentBinary(), Headless: posture.headless, Ask: posture.ask}
			wantLaunch, wantStream := buildAgentArgv(e, seed)
			rc := runCtxForPosture(seed, posture.headless, posture.ask)
			gotLaunch, gotStream := a.LaunchArgv(rc)
			if fmt.Sprint(gotLaunch) != fmt.Sprint(wantLaunch) {
				t.Errorf("%s/%s: registry LaunchArgv %v != switch %v", mode, posture.name, gotLaunch, wantLaunch)
			}
			if gotStream != wantStream {
				t.Errorf("%s/%s: registry stream %v != switch %v", mode, posture.name, gotStream, wantStream)
			}
		}
	}
}

// TestRegistryRecordMatchesManifest cross-checks the switch-less fields (Stream,
// Auth) against the embedded manifest; the test reads it, not the SPI packages.
func TestRegistryRecordMatchesManifest(t *testing.T) {
	m, err := loadAgentManifest()
	if err != nil {
		t.Fatalf("loadAgentManifest: %v", err)
	}
	for _, mode := range agentModes {
		a, ok := agents.Lookup(string(mode))
		if !ok {
			t.Errorf("registry Lookup(%q) missing", mode)
			continue
		}
		adapter, ok := m.adapter(string(mode))
		if !ok {
			t.Errorf("manifest missing agent %q", mode)
			continue
		}
		rec := a.Record()
		if rec.Binary != adapter.Binary {
			t.Errorf("%s: registry binary %q != manifest %q", mode, rec.Binary, adapter.Binary)
		}
		if rec.ContextLevel != adapter.ContextLevel {
			t.Errorf("%s: registry contextLevel %d != manifest %d", mode, rec.ContextLevel, adapter.ContextLevel)
		}
		if rec.Stream != adapter.Stream {
			t.Errorf("%s: registry stream %q != manifest %q", mode, rec.Stream, adapter.Stream)
		}
		if rec.Auth != adapter.Auth {
			t.Errorf("%s: registry auth %q != manifest %q", mode, rec.Auth, adapter.Auth)
		}
		if got := fmt.Sprint(rec.Argv.Headless); got != fmt.Sprint(adapter.Argv.Headless) {
			t.Errorf("%s: registry headless argv %v != manifest %v", mode, rec.Argv.Headless, adapter.Argv.Headless)
		}
		if got := fmt.Sprint(rec.Argv.Interactive); got != fmt.Sprint(adapter.Argv.Interactive) {
			t.Errorf("%s: registry interactive argv %v != manifest %v", mode, rec.Argv.Interactive, adapter.Argv.Interactive)
		}
	}
}

// TestRegistryQwenAlias pins the back-compat alias: Lookup("qwen") resolves to
// the opencode agent so the ward#401 rename does not hard-break --mode qwen.
func TestRegistryQwenAlias(t *testing.T) {
	a, ok := agents.Lookup("qwen")
	if !ok {
		t.Fatal("Lookup(\"qwen\") must resolve via the back-compat alias")
	}
	if a.Name() != "opencode" {
		t.Errorf("Lookup(\"qwen\").Name() = %q, want opencode", a.Name())
	}
	// parseMode aliases too, and both agree on the resolved mode.
	m, err := parseMode("qwen")
	if err != nil {
		t.Fatalf("parseMode(\"qwen\"): %v", err)
	}
	if string(m) != a.Name() {
		t.Errorf("parseMode(qwen) = %q but registry alias -> %q", m, a.Name())
	}
}
