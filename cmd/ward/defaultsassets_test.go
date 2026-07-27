package main

import (
	"reflect"
	"testing"
)

// TestDefaultsBundleParsesSplitLayout proves the split defaults/repos bundle
// contract loads through the runtime seam.
func TestPolicyBoundaryDefaultsBundleParsesSplitLayout(t *testing.T) {
	dir := writeBundleFixture(t)
	defs, err := loadSmartDefaultsFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadSmartDefaultsFrom(split bundle): %v", err)
	}
	if defs.repoAuthorityDefault != forgeForgejo {
		t.Fatalf("repo authority default = %v, want forgejo", defs.repoAuthorityDefault)
	}
	if len(defs.trustedOwners) == 0 || len(defs.repoAuthorityRules) == 0 {
		t.Fatalf("split bundle missing repo authority data: %+v", defs)
	}
}

func canonicalDefaultsBundleBytes(t *testing.T) []byte {
	t.Helper()
	b, err := bakedAssets.ReadFile(defaultsKDLPath)
	if err != nil {
		t.Fatalf("read canonical smart defaults: %v", err)
	}
	return b
}

func canonicalSmartDefaults(t *testing.T) smartDefaults {
	t.Helper()
	defs, err := parseSmartDefaultsBundle(canonicalDefaultsBundleBytes(t))
	if err != nil {
		t.Fatalf("parse canonical smart defaults: %v", err)
	}
	return defs
}

func TestPolicyBoundaryBakedSmartDefaultsDerivesFromCanonicalSource(t *testing.T) {
	want := canonicalSmartDefaults(t)
	got := bakedSmartDefaults()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("baked smart defaults no longer derive from the canonical source\nwant: %#v\ngot:  %#v", want, got)
	}
}
