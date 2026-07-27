package main

import (
	"testing"
)

// TestTopologyBundleParsesSplitLayout proves the topology file still loads from
// the bundle layout ward consumes.
func TestPolicyBoundaryTopologyBundleParsesSplitLayout(t *testing.T) {
	dir := writeBundleFixture(t)
	topo, err := loadContainerTopologyFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadContainerTopologyFrom(split bundle): %v", err)
	}
	if topo.TailnetNetwork == "" || topo.TowerHost == "" {
		t.Fatalf("parsed topology looks empty: %+v", topo)
	}
}
