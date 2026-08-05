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
	writeRepo(t, dest, "sample-platform", "# sample-platform\n\nEverything needed to stand up and operate the cluster.\n")
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
		"- **" + filepath.Join(dest, "sample-platform") + "** - Everything needed to stand up and operate the cluster.",
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

func TestRenderSubstrateInventoryUsesLocalDescriptionsOnly(t *testing.T) {
	dest := t.TempDir()
	writeRepo(t, dest, "alpha", "# alpha\n\nREADME owns this description.\n")
	writeRepo(t, dest, "beta", "")
	if err := os.WriteFile(filepath.Join(dest, "beta", "AGENTS.md"), []byte("# beta\n\nAGENTS owns this description.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRepo(t, dest, "gamma", "")
	if err := os.MkdirAll(filepath.Join(dest, "gamma", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "gamma", "docs", "FEATURES.md"), []byte("# gamma\n\nFEATURES owns this description.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRepo(t, dest, "omega", "")

	// A stale generated artifact is inert, whether left beside the mounts or in
	// the configured seed. Inventory derives solely from mounted directories.
	stale := `{"schema":1,"repos":[{"full_name":"remote/alpha","description":"remote description"}]}`
	if err := os.WriteFile(filepath.Join(dest, "substrate-catalog.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	seed := t.TempDir()
	if err := os.WriteFile(filepath.Join(seed, "substrate-catalog.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARD_SUBSTRATE_SEED", seed)

	block := substrateInventoryBlock(dest)
	for _, want := range []string{
		"- **" + filepath.Join(dest, "alpha") + "** - README owns this description.",
		"- **" + filepath.Join(dest, "beta") + "** - AGENTS owns this description.",
		"- **" + filepath.Join(dest, "gamma") + "** - FEATURES owns this description.",
		"- **" + filepath.Join(dest, "omega") + "**",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q\n---\n%s", want, block)
		}
	}
	if strings.Contains(block, "remote description") || strings.Contains(block, "substrate-catalog.json") {
		t.Errorf("stale catalog affected the local inventory:\n%s", block)
	}
	// os.ReadDir is name-sorted, and inventory preserves that order.
	last := -1
	for _, name := range []string{"alpha", "beta", "gamma", "omega"} {
		pos := strings.Index(block, filepath.Join(dest, name))
		if pos <= last {
			t.Errorf("inventory is not stable name order at %s:\n%s", name, block)
		}
		last = pos
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
