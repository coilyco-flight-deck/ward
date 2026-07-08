package main

import (
	"bytes"
	"os"
	"testing"
)

// topologySrcPath is the canonical container-topology source, mirrored to
// topologyassets by `make sync-topology-assets`.
const topologySrcPath = "../../.ward/ward-kdl/ward-kdl.topology.kdl"

// TestTopologyAssetsMirrorWardKDL fails when the embedded topology.generated.kdl
// drifts from the canonical source.
func TestTopologyAssetsMirrorWardKDL(t *testing.T) {
	src, err := os.ReadFile(topologySrcPath)
	if err != nil {
		t.Fatalf("read topology source %s: %v", topologySrcPath, err)
	}
	baked, err := bakedAssets.ReadFile(topologyGeneratedKDLPath)
	if err != nil {
		t.Fatalf("read baked %s: %v", topologyGeneratedKDLPath, err)
	}
	if !bytes.Equal(src, baked) {
		t.Errorf("embedded topologyassets/topology.generated.kdl has drifted from %s; re-sync with `make sync-topology-assets`", topologySrcPath)
	}
}
