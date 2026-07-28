package main

import (
	"strings"
	"testing"
)

func TestAgentAttribution(t *testing.T) {
	t.Setenv(envAgentDisplayName, "")
	t.Setenv(envAgentPronouns, "")
	cases := map[containerMode]string{
		modeClaude:   "Claude (she/her)",
		modeCodex:    "Codex",
		modeOpencode: "Qwen", // opencode harness signs as its backing model persona
		modeGoose:    "Goose",
	}
	for mode, want := range cases {
		if got := mode.agentAttribution(); got != want {
			t.Errorf("%s.agentAttribution() = %q, want %q", mode, got, want)
		}
	}
	// An unknown mode falls back to the claude identity rather than panicking.
	if got := containerMode("nope").agentAttribution(); got != "Claude (she/her)" {
		t.Errorf("unknown mode attribution = %q, want claude fallback", got)
	}
}

func TestSignBodyAppendsAttribution(t *testing.T) {
	t.Setenv(envAgentDisplayName, "")
	t.Setenv(envAgentPronouns, "")
	body := modeClaude.signBody("Here is the work.")
	for _, want := range []string{
		"Here is the work.",
		agentSignatureMarker,
		"Claude (she/her)",
		"`ward agent`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("signed body missing %q\n---\n%s", want, body)
		}
	}
	// The footer follows the original content, separated by a blank line.
	if !strings.HasPrefix(body, "Here is the work.\n\n") {
		t.Errorf("footer should trail the body after a blank line; got:\n%s", body)
	}
}

func TestSignBodyIsIdempotent(t *testing.T) {
	t.Setenv(envAgentDisplayName, "")
	t.Setenv(envAgentPronouns, "")
	once := modeClaude.signBody("payload")
	twice := modeClaude.signBody(once)
	if once != twice {
		t.Errorf("signing twice changed the body:\nonce:  %q\ntwice: %q", once, twice)
	}
	if n := strings.Count(twice, agentSignatureMarker); n != 1 {
		t.Errorf("idempotent sign must carry exactly one marker, got %d", n)
	}
	// A different mode must not re-sign a body already bearing the marker.
	if got := modeGoose.signBody(once); got != once {
		t.Errorf("re-signing under a new mode changed a marked body:\n%q", got)
	}
}

func TestSignBodyEmptyBecomesFooterOnly(t *testing.T) {
	t.Setenv(envAgentDisplayName, "")
	t.Setenv(envAgentPronouns, "")
	for _, in := range []string{"", "   \n\t"} {
		got := modeGoose.signBody(in)
		if !strings.Contains(got, agentSignatureMarker) || !strings.Contains(got, "Goose") {
			t.Errorf("empty body should sign to the footer alone; got %q", got)
		}
		if strings.HasPrefix(got, "\n") {
			t.Errorf("footer-only body must not lead with a blank line; got %q", got)
		}
	}
}

func TestCommitTrailer(t *testing.T) {
	t.Setenv(envAgentDisplayName, "")
	t.Setenv(envAgentPronouns, "")
	got := modeClaude.commitTrailer()
	want := "Co-Authored-By: Claude (she/her) <claude@ward.agent>"
	if got != want {
		t.Errorf("commitTrailer() = %q, want %q", got, want)
	}
	if got := modeGoose.commitTrailer(); got != "Co-Authored-By: Goose <goose@ward.agent>" {
		t.Errorf("goose commitTrailer() = %q", got)
	}
}

func TestAgentSignerUsesResolvedRoleIdentity(t *testing.T) {
	t.Setenv(envAgentDisplayName, "terran engineer")
	t.Setenv(envAgentPronouns, "he")

	body := modeCodex.signBody("done")
	for _, want := range []string{"terran engineer (he)", "`ward agent`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("signed body missing %q\n%s", want, body)
		}
	}
	if got := modeCodex.commitTrailer(); got != "Co-Authored-By: terran engineer (he) <codex@ward.agent>" {
		t.Fatalf("commitTrailer() = %q", got)
	}
}

func TestCurrentAgentMode(t *testing.T) {
	cases := []struct {
		agent, mode string
		want        containerMode
	}{
		{"goose", "", modeGoose},
		{"", "codex", modeCodex},
		{"opencode", "claude", modeOpencode}, // WARD_AGENT wins.
		{"qwen", "claude", modeClaude},       // deprecated spellings now fall back to WARD_MODE.
		{"", "", modeClaude},                 // unset defaults to claude
		{"bogus", "", modeClaude},            // unrecognized defaults to claude
	}
	for _, c := range cases {
		t.Setenv("WARD_AGENT", c.agent)
		t.Setenv("WARD_MODE", c.mode)
		if got := currentAgentMode(); got != c.want {
			t.Errorf("currentAgentMode(WARD_AGENT=%q, WARD_MODE=%q) = %q, want %q",
				c.agent, c.mode, got, c.want)
		}
	}
}
