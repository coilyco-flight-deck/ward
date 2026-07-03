package main

import (
	"os"
	"path/filepath"
	"testing"
)

// catalogContextRepos reads the repo-local catalog.dependsOn into read-only context
// grants, de-duplicated and in declared order (ward#573; was advisor-only, ward#566).
func TestCatalogContextRepos(t *testing.T) {
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
	deps, err := loadRepoLocalCatalogDeps(root)
	if err != nil {
		t.Fatalf("loadRepoLocalCatalogDeps: %v", err)
	}
	if len(deps) != 2 || deps[0].slug() != "coilyco-flight-deck/cli-guard" || deps[1].slug() != "StrangeLoopGames/Eco" {
		t.Fatalf("ward deps = %+v, want cli-guard + Eco", deps)
	}
	if got := catalogContextRepos(root); len(got) != 2 || got[1].slug() != "StrangeLoopGames/Eco" {
		t.Fatalf("catalogContextRepos(ward) = %+v, want catalog deps", got)
	}
}

func TestLoadRepoLocalCatalogDepsPrefersWardOverCoily(t *testing.T) {
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
	deps, err := loadRepoLocalCatalogDeps(filepath.Join(root, "nested"))
	if err != nil {
		t.Fatalf("loadRepoLocalCatalogDeps: %v", err)
	}
	if len(deps) != 1 || deps[0].slug() != "coilyco-flight-deck/cli-guard" {
		t.Fatalf("ward deps = %+v, want cli-guard", deps)
	}
}

// resolveContextRepos drops the target, any repo already a writable --repo grant
// (the writable grant wins), and any repo already warmed as a /substrate reference.
func TestResolveContextReposDedupes(t *testing.T) {
	target := targetRepo{Owner: "coilyco-gaming", Name: "eco-ops"}
	auto := []targetRepo{
		{Owner: "coilyco-gaming", Name: "eco-ops"},         // the target: dropped
		{Owner: "StrangeLoopGames", Name: "Eco"},           // a writable grant: dropped
		{Owner: "coilyco-flight-deck", Name: "cli-guard"},  // a substrate repo: dropped
		{Owner: "coilyco-flight-deck", Name: "eco-protos"}, // kept
		{Owner: "coilyco-flight-deck", Name: "eco-protos"}, // dup of the above: dropped
	}
	explicit := []targetRepo{{Owner: "StrangeLoopGames", Name: "Eco"}}
	substrate := map[string]bool{"coilyco-flight-deck/cli-guard": true}

	got, notes := resolveContextRepos(auto, explicit, target, substrate)
	if len(got) != 1 || got[0].slug() != "coilyco-flight-deck/eco-protos" {
		t.Fatalf("resolveContextRepos = %+v, want only eco-protos", got)
	}
	if len(notes) != 1 || notes[0].Reason != "read-only catalog dependency" {
		t.Fatalf("resolveContextRepos notes = %+v, want one read-only catalog dependency note", notes)
	}
}

// A read-only context repo also handed a writable --repo grant is cloned once, as
// the writable copy: the container-side dropExtraRepos filters the overlap out.
func TestDropExtraRepos(t *testing.T) {
	in := []targetRepo{
		{Owner: "coilyco-flight-deck", Name: "cli-guard"},
		{Owner: "coilyco-flight-deck", Name: "eco-protos"},
	}
	drop := []targetRepo{{Owner: "coilyco-flight-deck", Name: "eco-protos"}}
	got := dropExtraRepos(in, drop)
	if len(got) != 1 || got[0].slug() != "coilyco-flight-deck/cli-guard" {
		t.Fatalf("dropExtraRepos = %+v, want only cli-guard", got)
	}
}

// The read-only context set rides its own WARD_CONTEXT_REPOS key, never the
// writable WARD_EXTRA_REPOS, so the reaper's push-verify never sees it (ward#573).
func TestWardEnvContextReposSeparateKey(t *testing.T) {
	p := sampleUpPlan()
	p.ExtraRepos = []targetRepo{{Owner: "coilyco-gaming", Name: "eco-protos"}}
	p.ContextRepos = []targetRepo{{Owner: "coilyco-flight-deck", Name: "cli-guard"}}
	env := p.wardEnv()
	if got := env["WARD_EXTRA_REPOS"]; got != "coilyco-gaming/eco-protos" {
		t.Fatalf("WARD_EXTRA_REPOS = %q, want the writable grant only", got)
	}
	if got := env["WARD_CONTEXT_REPOS"]; got != "coilyco-flight-deck/cli-guard" {
		t.Fatalf("WARD_CONTEXT_REPOS = %q, want the read-only context repo", got)
	}
}
