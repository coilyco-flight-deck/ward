package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseReplyThoroughness(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty defaults to standard", "", defaultReplyThoroughness, false},
		{"whitespace defaults", "   ", defaultReplyThoroughness, false},
		{"quick", "quick", "quick", false},
		{"standard", "standard", "standard", false},
		{"deep", "deep", "deep", false},
		{"case-insensitive", "DEEP", "deep", false},
		{"trimmed", "  quick  ", "quick", false},
		{"unknown errors", "exhaustive", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lvl, err := parseReplyThoroughness(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseReplyThoroughness(%q): want error, got %q", c.in, lvl.Name)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReplyThoroughness(%q): unexpected error %v", c.in, err)
			}
			if lvl.Name != c.want {
				t.Errorf("parseReplyThoroughness(%q) = %q, want %q", c.in, lvl.Name, c.want)
			}
			if lvl.Timeout <= 0 {
				t.Errorf("parseReplyThoroughness(%q): timeout must be positive, got %s", c.in, lvl.Timeout)
			}
			if strings.TrimSpace(lvl.Guidance) == "" {
				t.Errorf("parseReplyThoroughness(%q): empty guidance", c.in)
			}
		})
	}
}

// The depth ladder should get strictly more wall-clock as it digs deeper.
func TestReplyThoroughnessTimeoutsClimb(t *testing.T) {
	var prev time.Duration
	for i, lvl := range replyThoroughnessLevels {
		if i > 0 && lvl.Timeout <= prev {
			t.Errorf("level %q timeout %s not greater than the prior %s", lvl.Name, lvl.Timeout, prev)
		}
		prev = lvl.Timeout
	}
}

func TestReplyResearchPrompt(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 179}
	level, _ := parseReplyThoroughness("deep")
	comments := []issueComment{
		{Body: "a human said this", User: struct {
			Login string `json:"login"`
		}{Login: "alice"}},
		// ward's own bookkeeping must be stripped from the woven thread.
		{Body: "noise " + agentReservationMarker, User: struct {
			Login string `json:"login"`
		}{Login: "coilyco-ops"}},
	}
	got := replyResearchPrompt(ref, "Reply mode", "the body text", comments, "what would it take?", level)

	for _, want := range []string{
		"one-shot research",
		"NOT implementing",             // the read-only contract
		"becomes the issue comment",    // stdout-is-the-comment contract
		"coilyco-flight-deck/ward#179", // the ref
		ref.url(),                      // the URL
		"the body text",                // the issue body
		"a human said this",            // the surviving human comment
		"what would it take?",          // the operator's question
		level.Guidance,                 // the depth steer
		"coilyco-flight-deck/ward.git", // the clone URL it may investigate
	} {
		if !strings.Contains(got, want) {
			t.Errorf("research prompt missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, agentReservationMarker) {
		t.Errorf("research prompt leaked ward's reservation marker into the thread:\n%s", got)
	}
}

// An empty body and an empty thread degrade to readable placeholders, never blanks.
func TestReplyResearchPromptEmpties(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1}
	level, _ := parseReplyThoroughness("quick")
	got := replyResearchPrompt(ref, "", "", nil, "go", level)
	for _, want := range []string{"(untitled)", "(no description provided)", "(no comments yet)"} {
		if !strings.Contains(got, want) {
			t.Errorf("research prompt missing placeholder %q\n---\n%s", want, got)
		}
	}
}

func TestReplyComment(t *testing.T) {
	level, _ := parseReplyThoroughness("standard")
	got := replyComment(modeClaude, level, "what would it take?", "Here is the answer.")

	for _, want := range []string{
		"ward agent reply",      // header
		"one-shot **standard**", // the depth in the header
		"> what would it take?", // the quoted question
		"Here is the answer.",   // the research body, inline
		"ward#179",              // provenance
		"not a carried change",  // the verify-before-acting caveat
		replyReplyMarker,        // the hidden marker
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply comment missing %q\n---\n%s", want, got)
		}
	}
}

// A multi-line prompt must stay inside the markdown blockquote, not break out of it.
func TestReplyCommentMultilinePromptStaysQuoted(t *testing.T) {
	level, _ := parseReplyThoroughness("quick")
	got := replyComment(modeClaude, level, "line one\nline two", "answer")
	if !strings.Contains(got, "> line one\n> line two") {
		t.Errorf("multi-line prompt broke out of the blockquote:\n%s", got)
	}
}

// reply signs under the driving mode's identity via commentIssue->signBody; the
// comment body itself must carry no signature (signing happens at send time).
func TestReplyCommentUnsigned(t *testing.T) {
	level, _ := parseReplyThoroughness("deep")
	got := replyComment(modeGoose, level, "q", "a")
	if strings.Contains(got, agentSignatureMarker) {
		t.Errorf("reply comment should not pre-sign; signBody handles that at send time:\n%s", got)
	}
	if !strings.Contains(got, "ward agent reply --driver goose") {
		t.Errorf("reply comment should name the driving mode:\n%s", got)
	}
}
