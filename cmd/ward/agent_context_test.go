package main

import (
	"os"
	"path/filepath"
	"strings"
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
    - acme/widgets
`), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write ward.yaml: %v", err)
	}
	deps, err := loadRepoLocalCatalogDeps(root)
	if err != nil {
		t.Fatalf("loadRepoLocalCatalogDeps: %v", err)
	}
	if len(deps) != 2 || deps[0].slug() != "coilyco-flight-deck/cli-guard" || deps[1].slug() != "acme/widgets" {
		t.Fatalf("ward deps = %+v, want cli-guard + widgets", deps)
	}
	if got := catalogContextRepos(root); len(got) != 2 || got[1].slug() != "acme/widgets" {
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
    - acme/widgets
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
		{Owner: "acme", Name: "widgets"},                   // a writable grant: dropped
		{Owner: "coilyco-flight-deck", Name: "cli-guard"},  // a substrate repo: dropped
		{Owner: "coilyco-flight-deck", Name: "eco-protos"}, // kept
		{Owner: "coilyco-flight-deck", Name: "eco-protos"}, // dup of the above: dropped
	}
	explicit := []targetRepo{{Owner: "acme", Name: "widgets"}}
	substrate := map[string]bool{"coilyco-flight-deck/cli-guard": true}

	got, notes := resolveContextRepos(depsOf(auto), explicit, target, substrate)
	if len(got) != 1 || got[0].slug() != "coilyco-flight-deck/eco-protos" {
		t.Fatalf("resolveContextRepos = %+v, want only eco-protos", got)
	}
	if len(notes) != 1 || notes[0].Reason != "read-only catalog dependency" {
		t.Fatalf("resolveContextRepos notes = %+v, want one read-only catalog dependency note", notes)
	}
}

// depsOf lifts a plain owner/name list into the internal catalog dependency type
// (all Forgejo-internal) so the dedup test can exercise resolveContextRepos.
func depsOf(repos []targetRepo) []catalogContextRepo {
	out := make([]catalogContextRepo, len(repos))
	for i, r := range repos {
		out[i] = catalogContextRepo{targetRepo: r}
	}
	return out
}

// parseCatalogDep honors a full clone URL's host + transport, classifying a
// non-Forgejo dep as external with its declared/synthesized clone URL (ward#612).
func TestParseCatalogDep(t *testing.T) {
	cases := []struct {
		dep      string
		slug     string
		external bool
		host     string
		cloneURL string
	}{
		{"coilyco-flight-deck/cli-guard", "coilyco-flight-deck/cli-guard", false, "", ""},
		{"forgejo.coilysiren.me/coilyco-flight-deck/eco-protos", "coilyco-flight-deck/eco-protos", false, "", ""},
		{"ssh://git@github.com/acme/widgets.git", "acme/widgets", true, "github.com", "ssh://git@github.com/acme/widgets.git"},
		{"github.com/acme/widgets", "acme/widgets", true, "github.com", "ssh://git@github.com/acme/widgets.git"},
		{"git@github.com:acme/widgets.git", "acme/widgets", true, "github.com", "git@github.com:acme/widgets.git"},
		{"https://gitlab.com/group/proj.git", "group/proj", true, "gitlab.com", "https://gitlab.com/group/proj.git"},
	}
	for _, c := range cases {
		got, err := parseCatalogDep(c.dep)
		if err != nil {
			t.Fatalf("parseCatalogDep(%q): %v", c.dep, err)
		}
		if got.slug() != c.slug {
			t.Errorf("parseCatalogDep(%q).slug = %q, want %q", c.dep, got.slug(), c.slug)
		}
		if got.external() != c.external {
			t.Errorf("parseCatalogDep(%q).external = %v, want %v", c.dep, got.external(), c.external)
		}
		if got.Host != c.host {
			t.Errorf("parseCatalogDep(%q).Host = %q, want %q", c.dep, got.Host, c.host)
		}
		if got.CloneURL != c.cloneURL {
			t.Errorf("parseCatalogDep(%q).CloneURL = %q, want %q", c.dep, got.CloneURL, c.cloneURL)
		}
	}
}

// An external dependsOn entry keeps its host + transport all the way from parse
// through resolve, and the WARD_CONTEXT_REPOS encoding round-trips it (ward#612).
func TestExternalCatalogDepEndToEnd(t *testing.T) {
	work := t.TempDir()
	wardDir := filepath.Join(work, ".ward")
	if err := os.MkdirAll(wardDir, 0o755); err != nil {
		t.Fatalf("mkdir ward: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wardDir, "ward.yaml"), []byte(`catalog:
  dependsOn:
    - ssh://git@github.com/acme/widgets.git
    - coilyco-flight-deck/eco-protos
`), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write ward.yaml: %v", err)
	}
	target := targetRepo{Owner: "coilyco-gaming", Name: "eco-ops"}
	repos, notes := resolveCatalogContextRepos(work, target, nil)
	if len(repos) != 2 {
		t.Fatalf("resolveCatalogContextRepos = %+v, want Eco + eco-protos", repos)
	}
	eco := repos[0]
	if !eco.external() || eco.Host != "github.com" || eco.CloneURL != "ssh://git@github.com/acme/widgets.git" {
		t.Fatalf("widgets dep = %+v, want external github.com over ssh", eco)
	}
	if eco.slug() != "acme/widgets" {
		t.Fatalf("widgets slug = %q, want acme/widgets", eco.slug())
	}
	if !strings.Contains(notes[0].Reason, "external") || !strings.Contains(notes[0].Reason, "github.com") {
		t.Fatalf("widgets note = %q, want an external github.com reason", notes[0].Reason)
	}
	// The env encoding must carry the honored URL, and re-parse back to the same dep.
	env := contextReposEnv(repos)
	want := "acme/widgets=ssh://git@github.com/acme/widgets.git coilyco-flight-deck/eco-protos"
	if env != want {
		t.Fatalf("contextReposEnv = %q, want %q", env, want)
	}
	back := parseContextReposEnv(env, target.Owner, target.Name)
	if len(back) != 2 || !back[0].external() || back[0].CloneURL != eco.CloneURL || back[0].Host != "github.com" {
		t.Fatalf("parseContextReposEnv round-trip = %+v, want widgets external preserved", back)
	}
	if back[1].external() || back[1].slug() != "coilyco-flight-deck/eco-protos" {
		t.Fatalf("parseContextReposEnv[1] = %+v, want internal eco-protos", back[1])
	}
}

// resolveCatalogContextRepos resolves the read-only context set from the fresh
// clone dir - not the host cwd (ward#580) - deduping the target and writable grants.
func TestResolveCatalogContextReposFromClone(t *testing.T) {
	work := t.TempDir()
	wardDir := filepath.Join(work, ".ward")
	if err := os.MkdirAll(wardDir, 0o755); err != nil {
		t.Fatalf("mkdir ward: %v", err)
	}
	// A full-host substrate ref (cli-guard, must drop), the target (must drop), a
	// writable grant (must drop), and one kept upstream (eco-protos).
	if err := os.WriteFile(filepath.Join(wardDir, "ward.yaml"), []byte(`catalog:
  dependsOn:
    - forgejo.coilysiren.me/coilyco-flight-deck/cli-guard
    - coilyco-gaming/eco-ops
    - acme/widgets
    - coilyco-flight-deck/eco-protos
`), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write ward.yaml: %v", err)
	}
	target := targetRepo{Owner: "coilyco-gaming", Name: "eco-ops"} // must drop (the target)
	extra := []targetRepo{{Owner: "acme", Name: "widgets"}}        // must drop (writable grant)
	repos, notes := resolveCatalogContextRepos(work, target, extra)
	if len(repos) != 1 || repos[0].slug() != "coilyco-flight-deck/eco-protos" {
		t.Fatalf("resolveCatalogContextRepos = %+v, want only eco-protos", repos)
	}
	if len(notes) != 1 || notes[0].Reason != "read-only catalog dependency" {
		t.Fatalf("notes = %+v, want one read-only catalog dependency note", notes)
	}
}

// The host no longer resolves or passes the read-only context set: it is resolved
// in-container from the fresh clone, so wardEnv emits no WARD_CONTEXT_REPOS (ward#580).
func TestWardEnvEmitsNoContextRepos(t *testing.T) {
	p := sampleUpPlan()
	p.ExtraRepos = []targetRepo{{Owner: "coilyco-gaming", Name: "eco-protos"}}
	env := p.wardEnv()
	if got := env["WARD_EXTRA_REPOS"]; got != "coilyco-gaming/eco-protos" {
		t.Fatalf("WARD_EXTRA_REPOS = %q, want the writable grant only", got)
	}
	if _, ok := env["WARD_CONTEXT_REPOS"]; ok {
		t.Fatalf("WARD_CONTEXT_REPOS must not be emitted from the host (ward#580); got %q", env["WARD_CONTEXT_REPOS"])
	}
}
