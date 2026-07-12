package main

import (
	"strings"
	"testing"

	kdl "github.com/calico32/kdl-go"
)

func burndownDefaults(t *testing.T, src string) smartDefaults {
	t.Helper()
	doc, err := kdl.ParseString(src)
	if err != nil {
		t.Fatalf("parse kdl: %v", err)
	}
	var defs smartDefaults
	for _, n := range doc.Nodes {
		if err := parseBundleReposNode("repos.kdl", n, &defs); err != nil {
			t.Fatalf("parseBundleReposNode: %v", err)
		}
	}
	return defs
}

const burndownAuthority = `    repo-authority default="forgejo" {
        trusted-owner coilyco-flight-deck
        repo "coilyco-flight-deck/*" forge="forgejo"
    }
`

func TestBurndownDefaultAndPerRepoOverride(t *testing.T) {
	defs := burndownDefaults(t, "repos {\n"+burndownAuthority+`
    burndown default=#true {
        repo "coilyco-flight-deck/infrastructure" #false
    }
}
`)
	if !defs.burndownEnabled("coilyco-flight-deck/ward") {
		t.Error("a repo with no entry should fall back to the block default (#true)")
	}
	if defs.burndownEnabled("coilyco-flight-deck/infrastructure") {
		t.Error("an explicit #false must exclude the repo from burndown")
	}
}

func TestBurndownDefaultFalseInvertsTheFallback(t *testing.T) {
	defs := burndownDefaults(t, "repos {\n"+burndownAuthority+`
    burndown default=#false {
        repo "coilyco-flight-deck/ward" #true
    }
}
`)
	if defs.burndownEnabled("coilyco-flight-deck/agentic-os") {
		t.Error("default=#false must exclude repos with no entry")
	}
	if !defs.burndownEnabled("coilyco-flight-deck/ward") {
		t.Error("an explicit #true must opt the repo back in")
	}
}

func TestBurndownAbsentBlockExcludesNothing(t *testing.T) {
	// The zero value of a bool is false, so a bundle with no burndown block must
	// not fall through to "excluded" - that would silently drain the director's
	// entire scope on every bundle that predates the block.
	defs := burndownDefaults(t, "repos {\n"+burndownAuthority+"}\n")
	if !defs.burndownEnabled("coilyco-flight-deck/ward") {
		t.Error("with no burndown block, no repo is excluded")
	}
}

func TestBurndownRejectsBareBooleans(t *testing.T) {
	// KDL v2 booleans are #true / #false. A bare true lexes as an identifier,
	// which is the class of bug that broke the whole bundle (aos repos.kdl).
	doc, err := kdl.ParseString("repos {\n" + burndownAuthority + `
    burndown default=#true {
        repo "coilyco-flight-deck/infrastructure" "false"
    }
}
`)
	if err != nil {
		t.Fatalf("parse kdl: %v", err)
	}
	var defs smartDefaults
	for _, n := range doc.Nodes {
		err = parseBundleReposNode("repos.kdl", n, &defs)
	}
	if err == nil {
		t.Fatal("a quoted \"false\" must be rejected, not read as a boolean")
	}
	if !strings.Contains(err.Error(), "#true or #false") {
		t.Errorf("error should name the KDL v2 spelling, got: %v", err)
	}
}

func TestBurndownRejectsUnknownNodeAndDuplicates(t *testing.T) {
	for name, src := range map[string]string{
		"unknown node":    "repos {\n" + burndownAuthority + "    burndown default=#true {\n        exclude \"a/b\"\n    }\n}\n",
		"duplicate":       "repos {\n" + burndownAuthority + "    burndown default=#true {\n        repo \"a/b\" #false\n        repo \"a/b\" #true\n    }\n}\n",
		"missing default": "repos {\n" + burndownAuthority + "    burndown {\n        repo \"a/b\" #false\n    }\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := kdl.ParseString(src)
			if err != nil {
				t.Fatalf("parse kdl: %v", err)
			}
			var defs smartDefaults
			for _, n := range doc.Nodes {
				err = parseBundleReposNode("repos.kdl", n, &defs)
			}
			if err == nil {
				t.Fatal("expected a fail-closed error")
			}
		})
	}
}
