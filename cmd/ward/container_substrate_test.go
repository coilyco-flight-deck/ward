package main

import "testing"

func TestParseSubstrateManifest(t *testing.T) {
	good := `# a comment
coilyco-flight-deck/ward              image

coilyco-bridge/lore                   cache
`
	repos, err := parseSubstrateManifest(good)
	if err != nil {
		t.Fatalf("parseSubstrateManifest(good) errored: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("want 2 repos, got %d: %+v", len(repos), repos)
	}
	if repos[0].slug() != "coilyco-flight-deck/ward" || repos[0].Tier != "image" {
		t.Errorf("first entry wrong: %+v", repos[0])
	}
	if repos[1].slug() != "coilyco-bridge/lore" || repos[1].Tier != "cache" {
		t.Errorf("second entry wrong: %+v", repos[1])
	}

	for _, bad := range []string{
		"coilyco-bridge/lore",             // missing tier
		"coilyco-bridge/lore cache extra", // too many fields
		"not-an-owner-name image",         // not owner/name
		"coilyco-flight-deck/ward warm",   // unknown tier
	} {
		if _, err := parseSubstrateManifest(bad); err == nil {
			t.Errorf("parseSubstrateManifest(%q): want error, got none", bad)
		}
	}
}

// TestEmbeddedSubstrateManifest guards the product-neutral default's syntax.
func TestEmbeddedSubstrateManifest(t *testing.T) {
	_, err := loadSubstrateManifest()
	if err != nil {
		t.Fatalf("embedded preclone-repos.txt does not parse: %v", err)
	}
}
