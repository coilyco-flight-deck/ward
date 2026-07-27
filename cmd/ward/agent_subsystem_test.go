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
func TestPolicyBoundaryMatchSubsystemPointers(t *testing.T) {
	// The ward#226 case: an issue whose whole point is a generated guardfile.
	hits := matchSubsystemPointers(wardRef(226), "wire an aosguard guardfile", "add an ops forgejo verb")
	if len(hits) == 0 {
		t.Fatalf("an AOSguard issue should match the ownership pointer; got none")
	}
	if hits[0].label == "" || len(hits[0].paths) == 0 {
		t.Errorf("matched pointer must carry a label and paths; got %+v", hits[0])
	}
	found := false
	for _, p := range hits[0].paths {
		if p == "docs/aosguard-boundary.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("AOSguard pointer should include docs/aosguard-boundary.md; got %v", hits[0].paths)
	}

	// A pointer fires once even when several of its keywords hit.
	dup := matchSubsystemPointers(wardRef(1), "aosguard guardfile ops forgejo", "")
	count := 0
	for _, p := range dup {
		if strings.HasPrefix(p.label, "AOSguard") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("a pointer with multiple matching keywords should fire once; fired %d times", count)
	}

	// Case-insensitive: keywords match regardless of issue casing.
	if got := matchSubsystemPointers(wardRef(2), "AOSGUARD Guardfile", ""); len(got) == 0 {
		t.Error("keyword match should be case-insensitive")
	}

	// A plain issue naming no known subsystem gets no pointers.
	if got := matchSubsystemPointers(wardRef(3), "tidy up a typo", "fix a comment"); len(got) != 0 {
		t.Errorf("an unrelated issue should match no pointers; got %v", got)
	}

	// Placeholder-adoption issues should light up the config/setup guidance.
	placeholder := matchSubsystemPointers(
		wardRef(1125),
		"Simple ward native pre flight agent that guides you in how to setup various placeholders, then prompts a restart",
		"help the user to adapt the container to their needs; Recursive invokes will be important here",
	)
	foundPlaceholder := false
	for _, p := range placeholder {
		if p.label != "placeholder setup + restart loop" {
			continue
		}
		foundPlaceholder = true
		for _, want := range []string{"docs/doctor.md", "docs/config-source.md"} {
			if !containsString(p.paths, want) {
				t.Errorf("placeholder pointer missing %q; got %v", want, p.paths)
			}
		}
		if !strings.Contains(p.followUp, "restart `warded`") {
			t.Errorf("placeholder pointer follow-up should prompt a restart; got %q", p.followUp)
		}
	}
	if !foundPlaceholder {
		t.Fatal("placeholder-adoption issue should match the placeholder setup pointer")
	}
}

// TestMatchSubsystemPointersScopedToWard covers ward#236's repo scoping: the map
// holds ward-specific paths, so a non-ward clone must get nothing.
func TestPolicyBoundaryMatchSubsystemPointersScopedToWard(t *testing.T) {
	other := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "cli-guard", Number: 9}
	if got := matchSubsystemPointers(other, "aosguard guardfile changes", ""); got != nil {
		t.Errorf("subsystem pointers must stay scoped to %s; a cli-guard issue got %v", subsystemPointerRepo, got)
	}
}

// TestSubsystemSeedBlock covers ward#236 item 1: a headless seed for an issue
// naming a subsystem must carry the front-load instruction and the paths.
func TestPolicyBoundarySubsystemSeedBlock(t *testing.T) {
	block := subsystemSeedBlock(wardRef(226), "aosguard guardfile", "ops forgejo verb")
	for _, want := range []string{
		"Front-load before you plan",
		"docs/aosguard-boundary.md",
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

	placeholder := subsystemSeedBlock(
		wardRef(1125),
		"Simple ward native pre flight agent that guides you in how to setup various placeholders, then prompts a restart",
		"help the user to adapt the container to their needs; Recursive invokes will be important here",
	)
	for _, want := range []string{
		"docs/doctor.md",
		"restart `warded`",
	} {
		if !strings.Contains(placeholder, want) {
			t.Errorf("placeholder seed block missing %q\n got: %s", want, placeholder)
		}
	}
}

// TestAgentSeedPromptFrontLoads covers ward#236 item 1 end-to-end: the seed the
// dispatcher hands the agent embeds the subsystem pointers when the issue names one.
func TestPolicyBoundaryAgentSeedPromptFrontLoads(t *testing.T) {
	ref := wardRef(236)
	got := agentSeedPrompt(ref, "feat(agent-dispatch): front-load subsystem context",
		"Scan the issue body for aosguard, guardfile, ward exec, headless keywords.", "", true, nil)
	for _, want := range []string{"Front-load before you plan", "docs/aosguard-boundary.md", "docs/agent.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("headless seed should front-load subsystem context; missing %q\n got: %s", want, got)
		}
	}
	// A plain ward issue keeps the original seed with no front-load block.
	plain := agentSeedPrompt(ref, "fix a typo", "just a wording change", "", true, nil)
	if strings.Contains(plain, "Front-load before you plan") {
		t.Errorf("a plain issue's seed should carry no front-load block; got: %s", plain)
	}
}

// TestPreflightPromptContextGate covers ward#236 item 2: the pre-flight demands a
// front-load list and surfaces the matched subsystem pointers.
func TestPolicyBoundaryPreflightPromptContextGate(t *testing.T) {
	got := preflightPrompt(wardRef(236), "front-load subsystem context",
		"scan for aosguard and guardfile keywords in headless dispatch", "", nil, nil)
	for _, want := range []string{
		"Context to front-load:", // the required checklist line
		"before your first edit", // the read-it-before-editing commitment
		"Naming a gap is not closing it",
		"docs/aosguard-boundary.md", // the matched pointer reaches the read
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
