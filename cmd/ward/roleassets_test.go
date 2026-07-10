package main

import (
	"bytes"
	"os"
	"testing"
)

// roleSrcPath is the canonical agent-role preset source mirrored into roleassets
// by `make sync-role-assets`.
const roleSrcPath = "../../.ward/ward-kdl/ward-kdl.roles.kdl"

// TestRoleAssetsMirrorWardKDL fails when the embedded roles.generated.kdl drifts
// from the canonical source.
func TestRoleAssetsMirrorWardKDL(t *testing.T) {
	src, err := os.ReadFile(roleSrcPath)
	if err != nil {
		t.Fatalf("read role source %s: %v", roleSrcPath, err)
	}
	baked, err := bakedAssets.ReadFile(rolesGeneratedKDLPath)
	if err != nil {
		t.Fatalf("read baked %s: %v", rolesGeneratedKDLPath, err)
	}
	if !bytes.Equal(src, baked) {
		t.Errorf("embedded roleassets/roles.generated.kdl has drifted from %s; re-sync with `make sync-role-assets`", roleSrcPath)
	}
}
