package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
)

// wardKdlSrcDir is the canonical ward-kdl guardfile directory the embedded
// execassets mirror (go:embed can't reach this sibling dir, so the build copies).
const wardKdlSrcDir = "../ward-kdl"

// TestExecAssetsMirrorWardKDL fails when execassets drifts from the canonical
// exec-dialect ward-kdl guardfiles. See docs/ward-kdl-in-ward.md.
func TestExecAssetsMirrorWardKDL(t *testing.T) {
	entries, err := os.ReadDir(wardKdlSrcDir)
	if err != nil {
		t.Fatalf("read ward-kdl source dir: %v", err)
	}
	wantExec := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "ward-kdl.") || !strings.HasSuffix(name, ".guardfile.kdl") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(wardKdlSrcDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		// execverb.Parse is the dialect discriminator: exec parses, spec errors.
		if _, perr := execverb.Parse(src); perr != nil {
			continue // spec dialect: not mirrored
		}
		wantExec[name] = true
		got, err := execAssets.ReadFile(execAssetsDir + "/" + name)
		if err != nil {
			t.Errorf("exec-dialect guardfile %s is not embedded under execassets; re-sync with `make sync-exec-assets`", name)
			continue
		}
		if !bytes.Equal(src, got) {
			t.Errorf("embedded execassets/%s has drifted from %s/%s; re-sync with `make sync-exec-assets`", name, wardKdlSrcDir, name)
		}
	}

	embedded, err := fs.ReadDir(execAssets, execAssetsDir)
	if err != nil {
		t.Fatalf("read embedded execassets: %v", err)
	}
	for _, e := range embedded {
		if e.IsDir() {
			continue
		}
		if !wantExec[e.Name()] {
			t.Errorf("execassets/%s is not a current exec-dialect ward-kdl guardfile; remove it and re-sync with `make sync-exec-assets`", e.Name())
		}
	}
	if len(wantExec) == 0 {
		t.Fatal("no exec-dialect ward-kdl guardfiles discovered; the mirror is empty")
	}
}
