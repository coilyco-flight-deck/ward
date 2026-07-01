package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOperatorLocal points $HOME at a temp dir and writes body to
// ~/.ward/fleet.local.kdl there (an empty body writes no file).
func writeOperatorLocal(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if body == "" {
		return
	}
	dir := filepath.Join(home, ".ward")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, operatorLocalConfigFile), []byte(body), 0o600); err != nil {
		t.Fatalf("write fleet.local.kdl: %v", err)
	}
}

func TestOperatorLocalConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := operatorLocalConfigPath()
	if err != nil {
		t.Fatalf("operatorLocalConfigPath: %v", err)
	}
	want := filepath.Join(home, ".ward", "fleet.local.kdl")
	if got != want {
		t.Errorf("operatorLocalConfigPath() = %q, want %q", got, want)
	}
}

// TestLoadOperatorLocalMissing is the common case: no host file yields the empty
// layer with no error, so an operator who never wrote one is not a failure.
func TestLoadOperatorLocalMissing(t *testing.T) {
	writeOperatorLocal(t, "")
	f, err := loadOperatorLocalConfig()
	if err != nil {
		t.Fatalf("loadOperatorLocalConfig: %v", err)
	}
	if f.Director != nil {
		t.Errorf("Director = %+v, want nil for a missing file", f.Director)
	}
	if f.SchemaVersion != 0 || len(f.Agents) != 0 {
		t.Errorf("missing file carried non-zero layer: %+v", f)
	}
}

// TestLoadOperatorLocalDirector reads the one node an operator-local source may
// set, confirming the shared parser hands ward a typed value.
func TestLoadOperatorLocalDirector(t *testing.T) {
	writeOperatorLocal(t, `director { default-scope "coilyco-flight-deck" }`)
	f, err := loadOperatorLocalConfig()
	if err != nil {
		t.Fatalf("loadOperatorLocalConfig: %v", err)
	}
	if f.Director == nil {
		t.Fatal("Director = nil, want the parsed director block")
	}
	if f.Director.DefaultScope != "coilyco-flight-deck" {
		t.Errorf("Director.DefaultScope = %q, want coilyco-flight-deck", f.Director.DefaultScope)
	}
}

// TestLoadOperatorLocalRejectsFleet proves the reader fails closed on the
// embed-only `fleet` block rather than silently accepting it.
func TestLoadOperatorLocalRejectsFleet(t *testing.T) {
	writeOperatorLocal(t, `fleet { schema-version 2; agent a { binary a } }`)
	_, err := loadOperatorLocalConfig()
	if err == nil {
		t.Fatal("loadOperatorLocalConfig accepted an embed-only `fleet` block, want a fail-closed error")
	}
	if !strings.Contains(err.Error(), "embed-only") {
		t.Errorf("error = %q, want an embed-only rejection", err)
	}
}

// TestLoadOperatorLocalMalformed proves a present-but-broken file fails closed
// rather than degrading to the empty layer, so a typo cannot silently vanish.
func TestLoadOperatorLocalMalformed(t *testing.T) {
	writeOperatorLocal(t, `director { default-scope`)
	if _, err := loadOperatorLocalConfig(); err == nil {
		t.Fatal("loadOperatorLocalConfig accepted malformed KDL, want a fail-closed error")
	}
}
