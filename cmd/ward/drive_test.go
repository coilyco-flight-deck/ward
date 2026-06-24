package main

import (
	"strings"
	"testing"
)

// drive is a top-level verb (the canonical machinery), not an `agent` surface.
func TestDriveIsTopLevelVerb(t *testing.T) {
	cmd := driveCommand()
	if cmd.Name != "drive" {
		t.Fatalf("driveCommand name = %q, want %q", cmd.Name, "drive")
	}
}

func TestParseDriveArgs(t *testing.T) {
	t.Run("harness then prompt", func(t *testing.T) {
		mode, prompt, err := parseDriveArgs([]string{"claude", "summarize", "the", "audit", "log"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mode != modeClaude {
			t.Errorf("mode = %q, want claude", mode)
		}
		if prompt != "summarize the audit log" {
			t.Errorf("prompt = %q, want the joined tail", prompt)
		}
	})

	t.Run("single quoted prompt arg", func(t *testing.T) {
		mode, prompt, err := parseDriveArgs([]string{"codex", "what does exec_gate.go enforce?"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mode != modeCodex {
			t.Errorf("mode = %q, want codex", mode)
		}
		if prompt != "what does exec_gate.go enforce?" {
			t.Errorf("prompt = %q", prompt)
		}
	})

	t.Run("no args names the harness choices", func(t *testing.T) {
		_, _, err := parseDriveArgs(nil)
		if err == nil || !strings.Contains(err.Error(), "no harness") {
			t.Fatalf("want a no-harness error, got %v", err)
		}
		if !strings.Contains(err.Error(), agentDriverChoices()) {
			t.Errorf("no-harness error should list the harness choices: %v", err)
		}
	})

	t.Run("unknown harness is rejected", func(t *testing.T) {
		_, _, err := parseDriveArgs([]string{"gptme", "do a thing"})
		if err == nil || !strings.Contains(err.Error(), "invalid harness") {
			t.Fatalf("want an invalid-harness error, got %v", err)
		}
	})

	t.Run("harness with no prompt is rejected", func(t *testing.T) {
		_, _, err := parseDriveArgs([]string{"claude", "   "})
		if err == nil || !strings.Contains(err.Error(), "no prompt") {
			t.Fatalf("want a no-prompt error, got %v", err)
		}
	})
}

// drivePrompt names the boundary, carries the prompt verbatim, frames the inline
// one-shot output contract, and points persistence-needing work at agent work.
func TestDrivePrompt(t *testing.T) {
	got := drivePrompt("list the deny rules in the forgejo guardfile")
	for _, want := range []string{
		"list the deny rules in the forgejo guardfile", // the prompt verbatim
		"warded agent",        // names the noun
		"cli-guard policy",    // names the boundary
		"audit log",           // names the audit trail
		"streams straight to", // the inline-to-terminal contract
		"one-shot",            // the ephemerality contract
		"ward agent work",     // where to go for persisted, merged work
	} {
		if !strings.Contains(got, want) {
			t.Errorf("drive prompt missing %q\n---\n%s", want, got)
		}
	}
}

// An empty prompt degrades to a readable placeholder, never a blank seed.
func TestDrivePromptEmpty(t *testing.T) {
	got := drivePrompt("   ")
	if !strings.Contains(got, "(no prompt given)") {
		t.Errorf("drive prompt missing the empty placeholder\n---\n%s", got)
	}
}

// A drive plan rides the same one-shot attached branch as ask (WARD_ASK=1): the
// seed, not the env, is what makes a drive run read-only or not.
func TestDrivePlanUsesAskBranch(t *testing.T) {
	p := sampleUpPlan()
	p.Ask = true
	if got := p.wardEnv()["WARD_ASK"]; got != "1" {
		t.Errorf("drive plan WARD_ASK = %q, want 1", got)
	}
	if _, ok := p.wardEnv()["WARD_HEADLESS"]; ok {
		t.Error("drive plan must not set WARD_HEADLESS (drive is attached, not detached)")
	}
}
