package main

import (
	"strings"
	"testing"
)

func TestParseAgentIssueRef(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantErr   bool
	}{
		{"coilyco-flight-deck/ward#98", "coilyco-flight-deck", "ward", 98, false},
		{"  coilyco-flight-deck/ward#98  ", "coilyco-flight-deck", "ward", 98, false},
		{forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98", "coilyco-flight-deck", "ward", 98, false},
		{forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98/", "coilyco-flight-deck", "ward", 98, false},
		{"", "", "", 0, true},
		{"coilyco-flight-deck/ward", "", "", 0, true},               // no #N
		{"coilyco-flight-deck/ward#0", "", "", 0, true},             // non-positive
		{"coilyco-flight-deck/ward#-3", "", "", 0, true},            // negative
		{"https://github.com/owner/repo/issues/1", "", "", 0, true}, // GitHub URL rejected
		{"not-a-ref", "", "", 0, true},
	}
	for _, c := range cases {
		got, err := parseAgentIssueRef(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseAgentIssueRef(%q): want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAgentIssueRef(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Owner != c.wantOwner || got.Repo != c.wantRepo || got.Number != c.wantNum {
			t.Errorf("parseAgentIssueRef(%q) = %s, want %s/%s#%d", c.in, got, c.wantOwner, c.wantRepo, c.wantNum)
		}
	}
}

func TestAgentIssueRefURL(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98}
	want := forgejoBaseURL + "/coilyco-flight-deck/ward/issues/98"
	if got := ref.url(); got != want {
		t.Errorf("url() = %q, want %q", got, want)
	}
	// A URL must round-trip back through the parser.
	back, err := parseAgentIssueRef(ref.url())
	if err != nil || back != ref {
		t.Errorf("url round-trip = %+v, %v; want %+v", back, err, ref)
	}
}

func TestAgentSeedPrompt(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 98}
	got := agentSeedPrompt(ref, "  container verb family  ")
	for _, want := range []string{
		"coilyco-flight-deck/ward#98",
		"container verb family",    // title, trimmed
		ref.url(),                  // the read-it-first URL
		"closes #98",               // the close trailer
		"read the full issue body", // first-action instruction
	} {
		if !strings.Contains(got, want) {
			t.Errorf("seed prompt missing %q\n got: %s", want, got)
		}
	}
	// An empty title degrades gracefully, never blank-quotes.
	if !strings.Contains(agentSeedPrompt(ref, "   "), "(untitled)") {
		t.Error("empty title should render as (untitled)")
	}
}

func TestOwnerAllowed(t *testing.T) {
	r := &Runner{}
	for _, ok := range []string{"coilysiren", "coilyco-bridge", "coilyco-flight-deck"} {
		if !r.ownerAllowed(ok) {
			t.Errorf("ownerAllowed(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"evilcorp", "", "Coilysiren"} {
		if r.ownerAllowed(bad) {
			t.Errorf("ownerAllowed(%q) = true, want false", bad)
		}
	}
}

// TestDockerCreateArgvSeedsAgentArgs verifies the seeded prompt rides as the
// in-container agent's argv: after the image, never as a -e env, never leaked.
func TestDockerCreateArgvSeedsAgentArgs(t *testing.T) {
	p := sampleUpPlan()
	seed := "Work on Forgejo issue coilyco-flight-deck/ward#98."
	p.AgentArgs = []string{seed}
	argv := dockerCreateArgv(p, "/tmp/ward-env-xyz")

	if argv[len(argv)-1] != seed {
		t.Errorf("seed must be the final arg (the agent's argv), got %q", argv[len(argv)-1])
	}
	// The image must sit immediately before the agent args, not at the end.
	imageIdx := -1
	for i, a := range argv {
		if a == p.Image {
			imageIdx = i
		}
	}
	if imageIdx == -1 || imageIdx != len(argv)-2 {
		t.Errorf("image must immediately precede the seeded agent args; image at %d of %d", imageIdx, len(argv))
	}
	// The seed must not have been turned into an env var.
	for _, a := range argv {
		if strings.HasPrefix(a, "WARD_") && strings.Contains(a, seed) {
			t.Errorf("seed prompt leaked into env arg %q", a)
		}
	}
}

// A bare up plan (no AgentArgs) still ends at the image - container up's shape.
func TestDockerCreateArgvNoAgentArgs(t *testing.T) {
	p := sampleUpPlan()
	p.AgentArgs = nil
	argv := dockerCreateArgv(p, "")
	if argv[len(argv)-1] != p.Image {
		t.Errorf("with no agent args the image must be the final arg, got %q", argv[len(argv)-1])
	}
}
