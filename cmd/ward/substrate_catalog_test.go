package main

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeRepoLister stands in for the Forgejo client: it returns canned repo lists
// per owner and records that each owner is queried exactly once.
type fakeRepoLister struct {
	byOwner map[string][]repoBrief
	calls   map[string]int
}

func (f *fakeRepoLister) listOwnerRepos(_ context.Context, owner string) ([]repoBrief, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[owner]++
	return f.byOwner[owner], nil
}

func TestBuildSubstrateCatalog(t *testing.T) {
	manifest := []substrateRepo{
		{Owner: "coilyco-flight-deck", Name: "infrastructure", Tier: "image"},
		{Owner: "coilyco-flight-deck", Name: "ward", Tier: "image"},
		{Owner: "coilyco-bridge", Name: "deploy", Tier: "cache"},
		// A manifest repo forgejo does not return still gets an entry.
		{Owner: "coilyco-flight-deck", Name: "ghost", Tier: "image"},
	}
	lister := &fakeRepoLister{byOwner: map[string][]repoBrief{
		"coilyco-flight-deck": {
			{Name: "infrastructure", FullName: "coilyco-flight-deck/infrastructure", Description: "k3s cluster", Topics: []string{"k3s", "homelab"}},
			{Name: "ward", FullName: "coilyco-flight-deck/ward", Description: "Harness driver"},
			{Name: "unrelated", FullName: "coilyco-flight-deck/unrelated", Description: "not on the manifest"},
		},
		"coilyco-bridge": {
			{Name: "deploy", FullName: "coilyco-bridge/deploy", Description: "deploy monorepo", Topics: []string{"helm"}},
		},
	}}

	cat, err := buildSubstrateCatalog(context.Background(), lister, manifest, "/substrate")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if cat.Schema != substrateCatalogSchema {
		t.Errorf("schema = %d, want %d", cat.Schema, substrateCatalogSchema)
	}
	// Each owner is listed exactly once even though it carries several manifest repos.
	if lister.calls["coilyco-flight-deck"] != 1 {
		t.Errorf("coilyco-flight-deck listed %d times, want 1", lister.calls["coilyco-flight-deck"])
	}
	// Entries sort by full_name, so ghost < infrastructure < ward, then deploy? No:
	// coilyco-bridge/deploy sorts before coilyco-flight-deck/*. Assert the order.
	wantOrder := []string{
		"coilyco-bridge/deploy",
		"coilyco-flight-deck/ghost",
		"coilyco-flight-deck/infrastructure",
		"coilyco-flight-deck/ward",
	}
	if len(cat.Repos) != len(wantOrder) {
		t.Fatalf("got %d entries, want %d: %+v", len(cat.Repos), len(wantOrder), cat.Repos)
	}
	for i, want := range wantOrder {
		if cat.Repos[i].FullName != want {
			t.Errorf("entry %d full_name = %q, want %q", i, cat.Repos[i].FullName, want)
		}
	}
	byName := map[string]catalogEntry{}
	for _, e := range cat.Repos {
		byName[e.FullName] = e
	}
	infra := byName["coilyco-flight-deck/infrastructure"]
	if infra.Description != "k3s cluster" {
		t.Errorf("infra description = %q", infra.Description)
	}
	if infra.MountPath != "/substrate/infrastructure" {
		t.Errorf("infra mount_path = %q", infra.MountPath)
	}
	if infra.Tier != "image" {
		t.Errorf("infra tier = %q", infra.Tier)
	}
	if len(infra.Topics) != 2 || infra.Topics[0] != "k3s" {
		t.Errorf("infra topics = %v", infra.Topics)
	}
	// The unrelated repo (owned but not on the manifest) is not in the catalog.
	if _, present := byName["coilyco-flight-deck/unrelated"]; present {
		t.Error("off-manifest repo leaked into the catalog")
	}
	// The ghost repo (on the manifest, not returned by forgejo) still gets an entry
	// off the manifest's own owner/name, with its tier and mount path.
	ghost := byName["coilyco-flight-deck/ghost"]
	if ghost.FullName != "coilyco-flight-deck/ghost" || ghost.MountPath != "/substrate/ghost" || ghost.Tier != "image" {
		t.Errorf("ghost entry = %+v", ghost)
	}
	if ghost.Description != "" {
		t.Errorf("ghost description = %q, want empty (no forgejo data)", ghost.Description)
	}
}

func TestRenderSubstrateCatalogStable(t *testing.T) {
	cat := substrateCatalog{Schema: 1, Repos: []catalogEntry{
		{FullName: "o/a", Description: "a", Tier: "image", MountPath: "/substrate/a"},
	}}
	data, err := renderSubstrateCatalog(cat)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if data[len(data)-1] != '\n' {
		t.Error("catalog should end with a trailing newline")
	}
	// It round-trips back to the same shape.
	var back substrateCatalog
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Schema != 1 || len(back.Repos) != 1 || back.Repos[0].FullName != "o/a" {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}
