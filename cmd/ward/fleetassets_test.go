package main

import (
	"bytes"
	"os"
	"testing"
)

// fleetSrcPath is the canonical dialect-2 fleet source; the embedded fleetassets
// copy is mirrored from it by `make sync-fleet-assets` (go:embed can't reach it).
const fleetSrcPath = "../ward-kdl/ward-kdl.fleet.kdl"

// TestFleetAssetsMirrorWardKDL fails when the embedded fleet.generated.kdl drifts
// from the canonical source - re-sync with `make sync-fleet-assets`.
func TestFleetAssetsMirrorWardKDL(t *testing.T) {
	src, err := os.ReadFile(fleetSrcPath)
	if err != nil {
		t.Fatalf("read fleet source %s: %v", fleetSrcPath, err)
	}
	if !bytes.Equal(src, fleetGeneratedKDL) {
		t.Errorf("embedded fleetassets/fleet.generated.kdl has drifted from %s; re-sync with `make sync-fleet-assets`", fleetSrcPath)
	}
}

// TestFleetConfigParses guards the embedded fleet config: it must parse and
// declare the supported schema version, so a broken embed is a red build here.
func TestFleetConfigParses(t *testing.T) {
	f, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig: %v", err)
	}
	if f.SchemaVersion == 0 {
		t.Error("parsed fleet has no schema version (missing `fleet` block?)")
	}
	if len(f.Agents) == 0 {
		t.Error("parsed fleet declares no agents")
	}
}
