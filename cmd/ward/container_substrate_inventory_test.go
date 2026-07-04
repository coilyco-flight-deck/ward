package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepo drops a fake substrate repo at dest/name with the given README body.
func writeRepo(t *testing.T, dest, name, readme string) {
	t.Helper()
	dir := filepath.Join(dest, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if readme != "" {
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
			t.Fatalf("write README: %v", err)
		}
	}
}

func TestSubstrateInventoryBlock(t *testing.T) {
	// A missing/empty substrate dir yields no section, so the composed context is
	// unchanged on a substrate-less run.
	if got := substrateInventoryBlock(filepath.Join(t.TempDir(), "absent")); got != "" {
		t.Errorf("absent dest: want empty block, got %q", got)
	}
	empty := t.TempDir()
	if got := substrateInventoryBlock(empty); got != "" {
		t.Errorf("empty dest: want empty block, got %q", got)
	}

	dest := t.TempDir()
	writeRepo(t, dest, "infrastructure", "# infrastructure\n\nEverything Kai needs to stand up and operate kai-server.\n")
	writeRepo(t, dest, "cli-guard", "# cli-guard\n\n[![badge][x]][y]\n\nThe policy and routing engine.\n")
	// A README that opens with a code fence and has no heading yields no tagline,
	// but the repo is still listed (bare path) so the mount is never hidden.
	writeRepo(t, dest, "coilysiren", "```\nascii art\n```\n")
	// A non-directory entry under /substrate is ignored.
	if err := os.WriteFile(filepath.Join(dest, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	block := substrateInventoryBlock(dest)
	if block == "" {
		t.Fatal("want a non-empty block for a warmed substrate dir")
	}
	for _, want := range []string{
		"read these BEFORE asking",
		"- **" + filepath.Join(dest, "infrastructure") + "** - Everything Kai needs to stand up and operate kai-server.",
		"- **" + filepath.Join(dest, "cli-guard") + "** - The policy and routing engine.",
		"- **" + filepath.Join(dest, "coilysiren") + "**",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q\n---\n%s", want, block)
		}
	}
	// The badge line must not leak into cli-guard's tagline.
	if strings.Contains(block, "badge") {
		t.Errorf("badge noise leaked into a tagline:\n%s", block)
	}
	// The stray file must not be listed.
	if strings.Contains(block, "stray") {
		t.Errorf("non-directory entry leaked into the block:\n%s", block)
	}
}

func TestRenderSubstrateInventoryWithCatalog(t *testing.T) {
	dest := t.TempDir()
	writeRepo(t, dest, "infrastructure", "# infra\n\nREADME tagline, should be overridden.\n")
	writeRepo(t, dest, "ward", "# ward\n\nREADME tagline for ward.\n")
	// A mounted repo with no catalog entry falls back to its README tagline.
	writeRepo(t, dest, "orphan", "# orphan\n\nOnly a README here.\n")

	catalog := map[string]catalogEntry{
		"infrastructure": {
			FullName:    "coilyco-flight-deck/infrastructure",
			Description: "k3s cluster and systemd units",
			Topics:      []string{"k3s", "homelab"},
			MountPath:   "/substrate/infrastructure",
		},
		// ward has an entry but no description: falls back to README, keeps full_name.
		"ward": {FullName: "coilyco-flight-deck/ward", Topics: []string{"agents"}},
	}

	block := renderSubstrateInventory(dest, catalog)
	for _, want := range []string{
		"- **" + filepath.Join(dest, "infrastructure") + "** (coilyco-flight-deck/infrastructure) - k3s cluster and systemd units [topics: k3s, homelab]",
		"- **" + filepath.Join(dest, "ward") + "** (coilyco-flight-deck/ward) - README tagline for ward. [topics: agents]",
		"- **" + filepath.Join(dest, "orphan") + "** - Only a README here.",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q\n---\n%s", want, block)
		}
	}
	// The overridden repo must show the catalog description, not the README tagline.
	if strings.Contains(block, "should be overridden") {
		t.Errorf("README tagline leaked past the catalog description:\n%s", block)
	}
}

func TestReadCatalogIndex(t *testing.T) {
	dir := t.TempDir()
	// A missing file yields a nil index (best-effort), not a panic.
	if idx := readCatalogIndex(filepath.Join(dir, "absent.json")); idx != nil {
		t.Errorf("absent catalog: want nil index, got %v", idx)
	}
	// Malformed JSON also yields nil, never a failure.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if idx := readCatalogIndex(bad); idx != nil {
		t.Errorf("malformed catalog: want nil index, got %v", idx)
	}
	// A well-formed catalog indexes by the full_name's last segment.
	good := filepath.Join(dir, "substrate-catalog.json")
	cat := substrateCatalog{Schema: 1, Repos: []catalogEntry{
		{FullName: "coilyco-flight-deck/infrastructure", Description: "d"},
	}}
	data, err := renderSubstrateCatalog(cat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, data, 0o644); err != nil {
		t.Fatal(err)
	}
	idx := readCatalogIndex(good)
	if e, ok := idx["infrastructure"]; !ok || e.Description != "d" {
		t.Errorf("index[infrastructure] = %+v, ok=%v", e, ok)
	}
}

func TestSubstrateRepoTagline(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name   string
		readme string
		want   string
	}{
		{"prose after heading", "# title\n\nThe first prose line.\n", "The first prose line."},
		{"skip badges", "# t\n[![a][b]][c]\n![img](x)\nReal tagline here.\n", "Real tagline here."},
		{"skip fence then prose", "```\ncode\n```\nAfter the fence.\n", "After the fence."},
		{"heading fallback", "# just a heading\n\n## another\n", "just a heading"},
		{"collapse wrapped whitespace", "# t\n\nwrapped    across   spaces\n", "wrapped across spaces"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := filepath.Join(dir, tc.name)
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(tc.readme), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := substrateRepoTagline(repo); got != tc.want {
				t.Errorf("tagline = %q, want %q", got, tc.want)
			}
		})
	}

	// AGENTS.md is the fallback source when README.md is absent.
	repo := filepath.Join(dir, "agents-only")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# a\n\nFrom AGENTS.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := substrateRepoTagline(repo); got != "From AGENTS." {
		t.Errorf("AGENTS fallback tagline = %q, want %q", got, "From AGENTS.")
	}
}
