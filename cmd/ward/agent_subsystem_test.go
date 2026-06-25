package main

import (
	"strings"
	"testing"
)

func wardRef(n int) agentIssueRef {
	return agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: n}
}

// TestMatchSubsystemPointers covers ward#236: a known subsystem keyword resolves
// to that subsystem's in-clone paths, firing once per pointer.
func TestMatchSubsystemPointers(t *testing.T) {
	// The ward#226 case: an issue whose whole point is a ward-kdl guardfile.
	hits := matchSubsystemPointers(wardRef(226), "wire a ward-kdl guardfile", "add an ops forgejo verb")
	if len(hits) == 0 {
		t.Fatalf("a ward-kdl issue should match the guardfile pointer; got none")
	}
	if hits[0].label == "" || len(hits[0].paths) == 0 {
		t.Errorf("matched pointer must carry a label and paths; got %+v", hits[0])
	}
	found := false
	for _, p := range hits[0].paths {
		if p == "docs/ward-kdl.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("ward-kdl pointer should include docs/ward-kdl.md; got %v", hits[0].paths)
	}

	// A pointer fires once even when several of its keywords hit.
	dup := matchSubsystemPointers(wardRef(1), "ward-kdl guardfile ops forgejo", "")
	count := 0
	for _, p := range dup {
		if strings.HasPrefix(p.label, "ward-kdl") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("a pointer with multiple matching keywords should fire once; fired %d times", count)
	}

	// Case-insensitive: keywords match regardless of issue casing.
	if got := matchSubsystemPointers(wardRef(2), "WARD-KDL Guardfile", ""); len(got) == 0 {
		t.Error("keyword match should be case-insensitive")
	}

	// A plain issue naming no known subsystem gets no pointers.
	if got := matchSubsystemPointers(wardRef(3), "tidy up a typo", "fix a comment"); len(got) != 0 {
		t.Errorf("an unrelated issue should match no pointers; got %v", got)
	}
}

// TestMatchSubsystemPointersScopedToWard covers ward#236's repo scoping: the map
// holds ward-specific paths, so a non-ward clone must get nothing.
func TestMatchSubsystemPointersScopedToWard(t *testing.T) {
	other := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "cli-guard", Number: 9}
	if got := matchSubsystemPointers(other, "ward-kdl guardfile changes", ""); got != nil {
		t.Errorf("subsystem pointers must stay scoped to %s; a cli-guard issue got %v", subsystemPointerRepo, got)
	}
}

// TestSubsystemSeedBlock covers ward#236 item 1: a headless seed for an issue
// naming a subsystem must carry the front-load instruction and the paths.
func TestSubsystemSeedBlock(t *testing.T) {
	block := subsystemSeedBlock(wardRef(226), "ward-kdl guardfile", "ops forgejo verb")
	for _, want := range []string{
		"Front-load before you plan",
		"docs/ward-kdl.md",
		"BEFORE your first edit",
		"is not", // the "located is not read" nudge
		"\"read\"",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("seed block missing %q\n got: %s", want, block)
		}
	}
	// No match -> empty block, so a plain issue's seed is untouched.
	if got := subsystemSeedBlock(wardRef(1), "tidy a typo", "nothing notable"); got != "" {
		t.Errorf("seed block should be empty when no subsystem matches; got: %s", got)
	}
}

// TestAgentSeedPromptFrontLoads covers ward#236 item 1 end-to-end: the seed the
// dispatcher hands the agent embeds the subsystem pointers when the issue names one.
func TestAgentSeedPromptFrontLoads(t *testing.T) {
	ref := wardRef(236)
	got := agentSeedPrompt(ref, "feat(agent-dispatch): front-load subsystem context",
		"Scan the issue body for ward-kdl, guardfile, ward exec, headless keywords.", "", modeClaude, true, nil)
	for _, want := range []string{"Front-load before you plan", "docs/ward-kdl.md", "docs/agent.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("headless seed should front-load subsystem context; missing %q\n got: %s", want, got)
		}
	}
	// A plain ward issue keeps the original seed with no front-load block.
	plain := agentSeedPrompt(ref, "fix a typo", "just a wording change", "", modeClaude, true, nil)
	if strings.Contains(plain, "Front-load before you plan") {
		t.Errorf("a plain issue's seed should carry no front-load block; got: %s", plain)
	}
}

// TestPreflightPromptContextGate covers ward#236 item 2: the pre-flight demands a
// front-load list and surfaces the matched subsystem pointers.
func TestPreflightPromptContextGate(t *testing.T) {
	got := preflightPrompt(wardRef(236), "front-load subsystem context",
		"scan for ward-kdl and guardfile keywords in headless dispatch", "", nil, nil)
	for _, want := range []string{
		"Context to front-load:", // the required checklist line
		"before your first edit", // the read-it-before-editing commitment
		"Naming a gap is not closing it",
		"docs/ward-kdl.md", // the matched pointer reaches the read
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preflight context gate missing %q\n got: %s", want, got)
		}
	}
	// The generic gate sentence applies even when no subsystem keyword matches, but
	// the subsystem-specific pointer block is omitted.
	plain := preflightPrompt(wardRef(1), "tidy a typo", "fix a wording nit", "", nil, nil)
	if !strings.Contains(plain, "Context to front-load:") {
		t.Errorf("the context-gate ask should apply to every pre-flight; got: %s", plain)
	}
	if strings.Contains(plain, "ward subsystems whose conventions live in the clone") {
		t.Errorf("a plain issue should carry no subsystem pointer block; got: %s", plain)
	}
}
