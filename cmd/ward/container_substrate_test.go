package main

import "testing"

func TestParseSubstrateManifest(t *testing.T) {
	good := `# a comment
coilyco-flight-deck/agentic-os        image

coilyco-bridge/lore                   cache
`
	repos, err := parseSubstrateManifest(good)
	if err != nil {
		t.Fatalf("parseSubstrateManifest(good) errored: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("want 2 repos, got %d: %+v", len(repos), repos)
	}
	if repos[0].slug() != "coilyco-flight-deck/agentic-os" || repos[0].Tier != "image" {
		t.Errorf("first entry wrong: %+v", repos[0])
	}
	if repos[1].slug() != "coilyco-bridge/lore" || repos[1].Tier != "cache" {
		t.Errorf("second entry wrong: %+v", repos[1])
	}

	for _, bad := range []string{
		"coilyco-bridge/lore",                 // missing tier
		"coilyco-bridge/lore cache extra",     // too many fields
		"not-an-owner-name image",             // not owner/name
		"coilyco-flight-deck/agentic-os warm", // unknown tier
	} {
		if _, err := parseSubstrateManifest(bad); err == nil {
			t.Errorf("parseSubstrateManifest(%q): want error, got none", bad)
		}
	}
}

// TestEmbeddedSubstrateManifest guards the committed manifest: it must parse and
// the public/private tier invariant must hold (image=public, cache=bridge).
func TestEmbeddedSubstrateManifest(t *testing.T) {
	repos, err := loadSubstrateManifest()
	if err != nil {
		t.Fatalf("embedded preclone-repos.txt does not parse: %v", err)
	}
	if len(repos) == 0 {
		t.Fatal("embedded manifest is empty")
	}
	publicOrgs := map[string]bool{"coilysiren": true, "coilyco-flight-deck": true}
	for _, r := range repos {
		switch r.Tier {
		case "image":
			if !publicOrgs[r.Owner] {
				t.Errorf("%s is image-tier but %q is not a public org - private content must not be baked into the shareable image", r.slug(), r.Owner)
			}
		case "cache":
			if r.Owner != "coilyco-bridge" {
				t.Errorf("%s is cache-tier from %q; cache tier is for leak-tolerant bridge repos", r.slug(), r.Owner)
			}
		}
	}
}
