package main

import (
	"strings"
	"testing"
)

// `ask` is a top-level agent surface alongside work/headless/task/reply (ward#185
// moved the harness onto --driver, so surfaces sit directly under `agent`).
func TestAgentHasAskSurface(t *testing.T) {
	surfaces := map[string]bool{}
	for _, c := range agentCommand().Commands {
		surfaces[c.Name] = true
	}
	if !surfaces["ask"] {
		t.Errorf("ward agent missing %q surface; got %v", "ask", surfaces)
	}
}

// askPrompt frames a read-only, inline, no-preamble answer and carries the
// question verbatim.
func TestAskPrompt(t *testing.T) {
	got := askPrompt("how does the reaper work?")
	for _, want := range []string{
		"how does the reaper work?", // the question verbatim
		"streams straight to a",     // the inline-to-terminal contract
		"no preamble",               // the clean-output contract
		"NOT implementing",          // the read-only contract
		"fresh clone of this repo",  // it may lean on the clone's context
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ask prompt missing %q\n---\n%s", want, got)
		}
	}
}

// An empty question degrades to a readable placeholder, never a blank prompt.
func TestAskPromptEmpty(t *testing.T) {
	got := askPrompt("   ")
	if !strings.Contains(got, "(no question given)") {
		t.Errorf("ask prompt missing the empty placeholder\n---\n%s", got)
	}
}

// An ask plan threads WARD_ASK=1 (and not WARD_HEADLESS) so the entrypoint picks
// the plain one-shot branch; a non-ask plan must not set it.
func TestWardEnvAsk(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_ASK"]; ok {
		t.Error("non-ask plan must not set WARD_ASK")
	}
	p.Ask = true
	if got := p.wardEnv()["WARD_ASK"]; got != "1" {
		t.Errorf("ask plan WARD_ASK = %q, want 1", got)
	}
	if _, ok := p.wardEnv()["WARD_HEADLESS"]; ok {
		t.Error("ask plan must not set WARD_HEADLESS (ask is attached, not detached/headless)")
	}
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	if !strings.Contains(joined, "-e WARD_ASK=1") {
		t.Errorf("docker argv missing -e WARD_ASK=1\n got: %s", joined)
	}
}

// ask stays attached: an ask plan keeps the interactive flag, never detaches (-d).
func TestAskPlanAttached(t *testing.T) {
	p := sampleUpPlan()
	p.Ask = true
	p.Interactive = true
	p.TTY = true
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	if strings.Contains(joined, " -d ") || strings.HasSuffix(joined, " -d") {
		t.Errorf("ask plan must not detach (-d)\n got: %s", joined)
	}
	if !strings.Contains(joined, "-it") {
		t.Errorf("attached ask plan with a TTY should pass -it\n got: %s", joined)
	}
}
