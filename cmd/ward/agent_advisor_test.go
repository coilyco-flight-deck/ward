package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `advisor` is a top-level role (ward#347): it answers without writing code, merging
// the retired reply + ask verbs (ref researches + comments, freeform answers inline).
func TestAgentHasAdvisorRole(t *testing.T) {
	surfaces := map[string]bool{}
	for _, c := range agentCommand().Commands {
		surfaces[c.Name] = true
	}
	if !surfaces["advisor"] {
		t.Errorf("ward agent missing %q role; got %v", "advisor", surfaces)
	}
	// The retired verbs are gone outright (hard rename, no aliases; ward#347).
	for _, gone := range []string{"reply", "ask"} {
		if surfaces[gone] {
			t.Errorf("retired verb %q must be gone after the advisor rename; got %v", gone, surfaces)
		}
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

// interactivePrompt frames a read-only, conversational session that invites
// follow-up and carries the question verbatim (ward#388, the freeform default).
func TestInteractivePrompt(t *testing.T) {
	got := interactivePrompt("how does the reaper work?")
	for _, want := range []string{
		"how does the reaper work?", // the question verbatim
		"interactive advisory",      // the conversational framing
		"follow-up",                 // invites back-and-forth
		"NOT an implementer",        // the read-only contract
		"fresh clone of this repo",  // it may lean on the clone's context
	} {
		if !strings.Contains(got, want) {
			t.Errorf("interactive prompt missing %q\n---\n%s", want, got)
		}
	}
	// It must NOT carry the one-shot "streams straight to a terminal / no preamble"
	// framing - that steer belongs to askPrompt, not the conversational session.
	if strings.Contains(got, "no preamble") {
		t.Errorf("interactive prompt should not carry the one-shot no-preamble steer\n---\n%s", got)
	}
}

// An empty question degrades to the same readable placeholder in the interactive path.
func TestInteractivePromptEmpty(t *testing.T) {
	got := interactivePrompt("   ")
	if !strings.Contains(got, "(no question given)") {
		t.Errorf("interactive prompt missing the empty placeholder\n---\n%s", got)
	}
}

// The advisor role exposes the --oneshot escape hatch (alias --answer) so scripting can
// force the one-shot answer even under a TTY (ward#388; --print stays the dry-run).
func TestAdvisorHasOneshotFlag(t *testing.T) {
	names := map[string]bool{}
	for _, f := range agentAdvisorFlags() {
		for _, n := range f.Names() {
			names[n] = true
		}
	}
	if !names["oneshot"] {
		t.Errorf("advisor flags missing --oneshot; got %v", names)
	}
	// --answer is the documented alias; Names() folds aliases in, so it lands in names too.
	if !names["answer"] {
		t.Errorf("advisor flags missing --answer alias for --oneshot; got %v", names)
	}
}

func TestAdvisorAutoGrantRepos(t *testing.T) {
	root := t.TempDir()
	wardDir := filepath.Join(root, ".ward")
	if err := os.MkdirAll(wardDir, 0o755); err != nil {
		t.Fatalf("mkdir ward: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wardDir, "ward.yaml"), []byte(`catalog:
  dependsOn:
    - coilyco-flight-deck/cli-guard
    - StrangeLoopGames/Eco
`), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write ward.yaml: %v", err)
	}
	deps, err := loadRepoLocalAutoGrantDeps(root)
	if err != nil {
		t.Fatalf("loadRepoLocalAutoGrantDeps: %v", err)
	}
	if len(deps) != 2 || deps[0].slug() != "coilyco-flight-deck/cli-guard" || deps[1].slug() != "StrangeLoopGames/Eco" {
		t.Fatalf("ward deps = %+v, want cli-guard + Eco", deps)
	}
	if got := advisorAutoGrantRepos(root); len(got) != 2 || got[1].slug() != "StrangeLoopGames/Eco" {
		t.Fatalf("advisorAutoGrantRepos(ward) = %+v, want catalog deps", got)
	}
}

func TestMergeExtraReposDedupes(t *testing.T) {
	target := targetRepo{Owner: "coilyco-gaming", Name: "eco-ops"}
	got, notes := mergeExtraRepos(
		[]targetRepo{{Owner: "StrangeLoopGames", Name: "Eco"}},
		[]targetRepo{{Owner: "StrangeLoopGames", Name: "Eco"}},
		target,
	)
	if len(got) != 1 || got[0].slug() != "StrangeLoopGames/Eco" {
		t.Fatalf("mergeExtraRepos dedupe = %+v, want one StrangeLoopGames/Eco", got)
	}
	if len(notes) != 1 {
		t.Fatalf("mergeExtraRepos notes = %+v, want one note", notes)
	}
	if notes[0].Reason != "explicit grant" {
		t.Fatalf("mergeExtraRepos note reason = %q, want explicit grant", notes[0].Reason)
	}
}

func TestLoadRepoLocalAutoGrantDepsPrefersWardOverCoily(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{".ward", ".coily"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".ward", "ward.yaml"), []byte(`catalog:
  dependsOn:
    - coilyco-flight-deck/cli-guard
`), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write ward.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".coily", "coily.yaml"), []byte(`catalog:
  dependsOn:
    - StrangeLoopGames/Eco
`), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write coily.yaml: %v", err)
	}
	deps, err := loadRepoLocalAutoGrantDeps(filepath.Join(root, "nested"))
	if err != nil {
		t.Fatalf("loadRepoLocalAutoGrantDeps: %v", err)
	}
	if len(deps) != 1 || deps[0].slug() != "coilyco-flight-deck/cli-guard" {
		t.Fatalf("ward deps = %+v, want cli-guard", deps)
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
