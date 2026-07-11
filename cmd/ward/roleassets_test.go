package main

import (
	"bytes"
	"os"
	"testing"
)

// roleSrcPath is the canonical role-definition source mirrored into roleassets
// by `make sync-role-assets`.
const roleSrcPath = "../../.ward/ward-kdl/ward-kdl.role-definitions.kdl"

// TestRoleAssetsMirrorWardKDL fails when the embedded role-definition asset
// drifts from the canonical source.
func TestRoleAssetsMirrorWardKDL(t *testing.T) {
	src, err := os.ReadFile(roleSrcPath)
	if err != nil {
		t.Fatalf("read role source %s: %v", roleSrcPath, err)
	}
	baked, err := bakedAssets.ReadFile(roleDefinitionsGeneratedKDLPath)
	if err != nil {
		t.Fatalf("read baked %s: %v", roleDefinitionsGeneratedKDLPath, err)
	}
	if !bytes.Equal(src, baked) {
		t.Errorf("embedded %s has drifted from %s; re-sync with `make sync-role-assets`", roleDefinitionsGeneratedKDLPath, roleSrcPath)
	}
}
