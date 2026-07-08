package main

import (
	"bytes"
	"os"
	"testing"
)

// defaultsSrcPath is the canonical smart-defaults source mirrored into
// defaultsassets by `make sync-defaults-assets`.
const defaultsSrcPath = "../../.ward/ward-kdl/ward-kdl.defaults.kdl"

// TestDefaultsAssetsMirrorWardKDL fails when the embedded defaults.generated.kdl
// drifts from the canonical source.
func TestDefaultsAssetsMirrorWardKDL(t *testing.T) {
	src, err := os.ReadFile(defaultsSrcPath)
	if err != nil {
		t.Fatalf("read defaults source %s: %v", defaultsSrcPath, err)
	}
	baked, err := bakedAssets.ReadFile(defaultsGeneratedKDLPath)
	if err != nil {
		t.Fatalf("read baked %s: %v", defaultsGeneratedKDLPath, err)
	}
	if !bytes.Equal(src, baked) {
		t.Errorf("embedded defaultsassets/defaults.generated.kdl has drifted from %s; re-sync with `make sync-defaults-assets`", defaultsSrcPath)
	}
}
