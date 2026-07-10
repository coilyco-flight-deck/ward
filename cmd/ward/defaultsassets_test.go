package main

import (
	"testing"
)

// TestDefaultsBundleParsesSplitLayout proves the split defaults/repos bundle
// contract loads through the runtime seam.
func TestDefaultsBundleParsesSplitLayout(t *testing.T) {
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
