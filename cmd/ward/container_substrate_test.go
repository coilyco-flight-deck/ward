package main

import "testing"

func TestParseSubstrateManifest(t *testing.T) {
	good := `# a comment
coilyco-flight-deck/ward              image

coilyco-gaming/lore                   cache
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
	if repos[1].slug() != "coilyco-gaming/lore" || repos[1].Tier != "cache" {
		t.Errorf("second entry wrong: %+v", repos[1])
	}

	for _, bad := range []string{
		"coilyco-gaming/lore",             // missing tier
		"coilyco-gaming/lore cache extra", // too many fields
		"not-an-owner-name image",         // not owner/name
		"coilyco-flight-deck/ward warm",   // unknown tier
	} {
		if _, err := parseSubstrateManifest(bad); err == nil {
			t.Errorf("parseSubstrateManifest(%q): want error, got none", bad)
		}
	}
}

// TestDefaultSubstrateManifest guards the product-neutral default: Ward ships
// only its public example repo, never a deployment's repository roster.
func TestDefaultSubstrateManifest(t *testing.T) {
	repos, err := loadSubstrateManifest()
	if err != nil {
		t.Fatalf("compiled-in preclone manifest does not parse: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("default manifest = %+v, want only coilysiren/example", repos)
	}
	if got := repos[0]; got.slug() != "coilysiren/example" || got.Tier != "image" {
		t.Fatalf("default manifest entry = %+v, want coilysiren/example image", got)
	}
}

func TestSubstrateRepoCloneURLUsesTypedRepoAuthority(t *testing.T) {
	e := bootstrapEnv{ForgejoBase: "https://forgejo.example"}
	if got := substrateRepoCloneURL(e, "coilysiren", "example"); got != "https://github.com/coilysiren/example.git" {
		t.Errorf("GitHub-authoritative substrate clone URL = %q", got)
	}
	if got := substrateRepoCloneURL(e, "coilyco-flight-deck", "ward"); got != "https://forgejo.example/coilyco-flight-deck/ward.git" {
		t.Errorf("Forgejo-authoritative substrate clone URL = %q", got)
	}
}
