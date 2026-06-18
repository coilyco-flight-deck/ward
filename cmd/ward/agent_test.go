package main

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
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

func TestTaskTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"add a --task flag", "add a --task flag"},
		{"\n\n  add a --task flag  \n\nmore body", "add a --task flag"}, // first non-empty line, trimmed
		{"", "agent task"},          // empty degrades, never blank
		{"   \n  \n", "agent task"}, // whitespace-only degrades too
		{strings.Repeat("x", 80), strings.Repeat("x", taskTitleMaxLen) + "…"}, // truncated + ellipsis
	}
	for _, c := range cases {
		if got := taskTitle(c.in); got != c.want {
			t.Errorf("taskTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// A truncated title must stay within the cap (plus the single ellipsis rune).
	if got := []rune(taskTitle(strings.Repeat("y", 200))); len(got) != taskTitleMaxLen+1 {
		t.Errorf("truncated title rune len = %d, want %d", len(got), taskTitleMaxLen+1)
	}
}

func TestTaskBody(t *testing.T) {
	got := taskBody(modeClaude, "do the thing")
	if !strings.Contains(got, "do the thing") {
		t.Error("body must carry the instructions verbatim")
	}
	if !strings.Contains(got, "ward agent claude task") {
		t.Errorf("body must mark provenance; got: %s", got)
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

// Headless threads WARD_HEADLESS=1 into the container env (the entrypoint runs
// claude -p); a non-headless plan must not set it.
func TestWardEnvHeadless(t *testing.T) {
	p := sampleUpPlan()
	if _, ok := p.wardEnv()["WARD_HEADLESS"]; ok {
		t.Error("non-headless plan must not set WARD_HEADLESS")
	}
	p.Headless = true
	if got := p.wardEnv()["WARD_HEADLESS"]; got != "1" {
		t.Errorf("headless plan WARD_HEADLESS = %q, want 1", got)
	}
	joined := strings.Join(dockerCreateArgv(p, ""), " ")
	if !strings.Contains(joined, "-e WARD_HEADLESS=1") {
		t.Errorf("docker argv missing -e WARD_HEADLESS=1\n got: %s", joined)
	}
}

// ward#141: goose is a first-class agent surface, so `ward agent goose
// {work,headless,task}` must exist alongside claude/codex/qwen.
func TestAgentModesIncludeGoose(t *testing.T) {
	found := false
	for _, m := range agentModes {
		if m == modeGoose {
			found = true
		}
	}
	if !found {
		t.Errorf("agentModes missing goose; got %v", agentModes)
	}
	// The umbrella command must build a `goose` subcommand with work/headless/task.
	var goose *cli.Command
	for _, c := range agentCommand().Commands {
		if c.Name == string(modeGoose) {
			goose = c
		}
	}
	if goose == nil {
		t.Fatal("agent command has no goose subcommand")
	}
	surfaces := map[string]bool{}
	for _, c := range goose.Commands {
		surfaces[c.Name] = true
	}
	for _, want := range []string{"work", "headless", "task"} {
		if !surfaces[want] {
			t.Errorf("ward agent goose missing %q surface", want)
		}
	}
}

// A goose headless plan threads both WARD_MODE=goose and WARD_HEADLESS=1 so the
// entrypoint picks the `goose run -t` branch.
func TestGooseHeadlessPlanEnv(t *testing.T) {
	p := sampleUpPlan()
	p.Mode = modeGoose
	p.Headless = true
	env := p.wardEnv()
	if env["WARD_MODE"] != "goose" {
		t.Errorf("WARD_MODE = %q, want goose", env["WARD_MODE"])
	}
	if env["WARD_AGENT"] != "goose" {
		t.Errorf("WARD_AGENT = %q, want goose", env["WARD_AGENT"])
	}
	if env["WARD_HEADLESS"] != "1" {
		t.Errorf("WARD_HEADLESS = %q, want 1", env["WARD_HEADLESS"])
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
