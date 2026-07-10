package main

import (
	"io/fs"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
)

// TestExecAssetsEmbeddedSmoke ensures the embedded exec assets are present and
// still parse as exec guardfiles after the bundle split.
func TestExecAssetsEmbeddedSmoke(t *testing.T) {
	entries, err := fs.ReadDir(bakedAssets, execAssetsDir)
	if err != nil {
		t.Fatalf("read embedded execassets: %v", err)
	}
	var execCount int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "" {
			t.Fatal("embedded execassets contains a blank entry name")
		}
		got, err := bakedAssets.ReadFile(execAssetsDir + "/" + e.Name())
		if err != nil {
			t.Fatalf("read embedded execassets/%s: %v", e.Name(), err)
		}
		if _, perr := execverb.Parse(got); perr != nil {
			t.Errorf("embedded execassets/%s no longer parses as an exec guardfile: %v", e.Name(), perr)
		}
		execCount++
	}
	if execCount == 0 {
		t.Fatal("no embedded exec-dialect ward-kdl guardfiles found")
	}
}
