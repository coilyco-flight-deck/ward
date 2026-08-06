package main

import (
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// manifestWithAuth builds a bare Manifest carrying just the auth kind + endpoint
// the trust gate keys off, for the fail-safe table.
func manifestWithAuth(auth, endpoint string) agentsapi.Manifest {
	return agentsapi.Manifest{Auth: auth, Endpoint: endpoint}
}

// agent_hostoneshot_test.go pins the ward#162 trust gate: ward's unsandboxed HOST
// one-shot reads are refused to a local-model harness. See docs/agent-lifecycle.md.

// TestHostOneShotTrustGate: cloud harnesses keep the host one-shot; local-model
// harnesses (ollama/none auth) are barred, argv and all (ward#162).
func TestHostOneShotTrustGate(t *testing.T) {
	cases := []struct {
		mode      containerMode
		wantTrust bool // may run a host one-shot at all
		wantArgv  bool // AND has a preflight argv wired (so hostOneShotArgv returns ok)
	}{
		{modeClaude, true, true},     // cloud, wired: the one host one-shot that survives
		{modeGoose, false, false},    // ollama-backed local model: barred (the ward#162 incident)
		{modeOpencode, false, false}, // local model, none-auth: barred
		{modeCodex, true, false},     // cloud, but no preflight argv wired: trusted yet ok=false
	}
	for _, tc := range cases {
		if got := hostOneShotTrusted(tc.mode); got != tc.wantTrust {
			t.Errorf("hostOneShotTrusted(%s) = %v, want %v", tc.mode, got, tc.wantTrust)
		}
		argv, ok := hostOneShotArgv(tc.mode, "carry it?")
		if ok != tc.wantArgv {
			t.Errorf("hostOneShotArgv(%s) ok = %v, want %v", tc.mode, ok, tc.wantArgv)
		}
		if !tc.wantArgv && argv != nil {
			t.Errorf("hostOneShotArgv(%s) returned argv %v for a barred/unwired mode; want nil", tc.mode, argv)
		}
		if tc.wantArgv {
			// A trusted, wired harness still gets its real argv with the prompt appended.
			if len(argv) == 0 || argv[len(argv)-1] != "carry it?" {
				t.Errorf("hostOneShotArgv(%s) = %v, want the prompt appended", tc.mode, argv)
			}
			if argv[0] != lookupAgent(tc.mode).Record().Binary {
				t.Errorf("hostOneShotArgv(%s) argv[0] = %q, want the binary %q", tc.mode, argv[0], lookupAgent(tc.mode).Record().Binary)
			}
		}
	}
}

// TestLocalModelAgentFailsafe: an unrecognized (empty) auth reads as local, so a
// new harness must opt IN with a cloud auth kind, not inherit host access (ward#162).
func TestLocalModelAgentFailsafe(t *testing.T) {
	local := []string{"ollama", "none", ""}
	for _, auth := range local {
		if !localModelAgent(manifestWithAuth(auth, "")) {
			t.Errorf("localModelAgent(auth=%q) = false, want true (local/fail-safe)", auth)
		}
	}
	// A cloud auth kind is trusted only when it names no local endpoint.
	if localModelAgent(manifestWithAuth("claude-keychain", "")) {
		t.Errorf("localModelAgent(claude-keychain) = true, want false (cloud)")
	}
	// A pinned provider endpoint tips even a named auth into local territory.
	if !localModelAgent(manifestWithAuth("codex-host", "http://localhost:11434/v1")) {
		t.Errorf("localModelAgent(endpoint set) = false, want true (local endpoint)")
	}
}
