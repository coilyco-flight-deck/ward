package main

import (
	"testing"
)

// TestFleetConfigParsesSplitBundle proves the new split bundle contract loads
// through the ward runtime seam.
func TestPolicyBoundaryFleetConfigParsesSplitBundle(t *testing.T) {
	dir := writeBundleFixture(t)
	f, err := loadFleetConfigFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadFleetConfigFrom(split bundle): %v", err)
	}
	if f.SchemaVersion == 0 {
		t.Error("parsed fleet has no schema version")
	}
	if len(f.Agents) == 0 {
		t.Error("parsed fleet declares no agents")
	}
	if len(f.Roles) == 0 {
		t.Error("parsed fleet declares no roles")
	}
}
