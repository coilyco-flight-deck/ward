package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// exampleSpecsDir is the neutral spec bundle a ward adopter starts from - the
// swappable build input (ward#453). See examples/ward-specs/README.md.
const exampleSpecsDir = "../../examples/ward-specs"

// coilycoTokens are the deployment-specific substrings the ward#441 sweep found
// compiled in. A build from the example bundle must carry none of them.
var coilycoTokens = []string{"coilysiren", "coily", "coilyco"}

// TestExampleBundleHasNoCoilycoValues is the ward#441 non-reproduction check: no
// example build-input file may carry a coilyco endpoint/token/owner value.
func TestExampleBundleHasNoCoilycoValues(t *testing.T) {
	entries, err := os.ReadDir(exampleSpecsDir)
	if err != nil {
		t.Fatalf("read example bundle dir %s: %v", exampleSpecsDir, err)
	}
	seen := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Only the compiled build inputs (guardfiles, fleet, spec locks) must be
		// deployment-agnostic. The README is prose about the bundle, not embedded.
		ext := filepath.Ext(e.Name())
		if ext != ".kdl" && ext != ".json" {
			continue
		}
		path := filepath.Join(exampleSpecsDir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		seen++
		lower := strings.ToLower(string(body))
		for _, tok := range coilycoTokens {
			if strings.Contains(lower, tok) {
				t.Errorf("example bundle file %s carries the coilyco-specific token %q; "+
					"the example must stay deployment-agnostic (ward#453)", e.Name(), tok)
			}
		}
	}
	if seen == 0 {
		t.Fatal("example bundle is empty; expected the neutral spec files under " + exampleSpecsDir)
	}
}

// TestExampleFleetParses proves the example bundle is a real, valid input, not
// just a coilyco-free blob: its dialect-2 fleet manifest parses (fleetconfig).
func TestExampleFleetParses(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(exampleSpecsDir, "ward-kdl.fleet.kdl"))
	if err != nil {
		t.Fatalf("read example fleet: %v", err)
	}
	f, err := fleetconfig.Parse(body)
	if err != nil {
		t.Fatalf("parse example fleet: %v", err)
	}
	if f.SchemaVersion == 0 {
		t.Error("example fleet has no schema version")
	}
	if len(f.Agents) == 0 {
		t.Error("example fleet declares no agents")
	}
}
