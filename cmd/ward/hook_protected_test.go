package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
)

func TestProtectedFor_Empty(t *testing.T) {
	if got := protectedFor(repocfg.Security{}); got != nil {
		t.Fatalf("empty Security -> nil, got %+v", got)
	}
}

func TestProtectedFor_ProtectedBinariesMapped(t *testing.T) {
	sec := repocfg.Security{
		ProtectedBinaries: []repocfg.ProtectedBinary{
			{Name: "gcloud", AllowedWrappers: []string{"kap"}},
		},
		Hooks: repocfg.HookPolicy{
			RouteHints: map[string]string{"gcloud": "Use kap for cloud operations."},
		},
	}
	got := protectedFor(sec)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(got), got)
	}
	if got[0].Name != "gcloud" || got[0].Hint != "Use kap for cloud operations." {
		t.Errorf("wrong mapping: %+v", got[0])
	}
	if len(got[0].Wrappers) != 1 || got[0].Wrappers[0] != "kap" {
		t.Errorf("wrappers not threaded: %+v", got[0].Wrappers)
	}
}

func TestProtectedFor_DenyBareSuppliesHintOnlyEntries(t *testing.T) {
	sec := repocfg.Security{
		Hooks: repocfg.HookPolicy{
			DenyBareBinaries: []string{"terraform"},
			RouteHints:       map[string]string{"terraform": "use `coily tf ...`."},
		},
	}
	got := protectedFor(sec)
	if len(got) != 1 || got[0].Name != "terraform" || got[0].Hint != "use `coily tf ...`." {
		t.Fatalf("want one hint-only terraform entry, got %+v", got)
	}
	if len(got[0].Wrappers) != 0 {
		t.Errorf("hint-only entry should have no wrappers: %+v", got[0])
	}
}

func TestProtectedFor_DenyBareSkipsCoveredNames(t *testing.T) {
	sec := repocfg.Security{
		ProtectedBinaries: []repocfg.ProtectedBinary{{Name: "gcloud"}},
		Hooks:             repocfg.HookPolicy{DenyBareBinaries: []string{"gcloud"}},
	}
	got := protectedFor(sec)
	if len(got) != 1 {
		t.Fatalf("want 1 (no dup), got %d: %+v", len(got), got)
	}
}

// fakeWardRepoWithBody is the security-aware counterpart of fakeRepo in
// hook_test.go: writes .ward/ward.yaml with the given body.
func fakeWardRepoWithBody(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".ward")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ward.yaml"), []byte(body), 0o644); err != nil { //nolint:gosec
		t.Fatalf("write: %v", err)
	}
	return root
}

const protectedYAML = `catalog:
  kind: Component
  type: tool
  system: t
  owner: kai
  lifecycle: production
  description: "test"
commands:
  noop:
    run: make noop
    description: noop
security:
  protected_binaries:
    - name: gcloud
      allowed_wrappers: [kap]
`

func runHookInCwd(t *testing.T, payload map[string]interface{}, cwd string) (string, int) {
	t.Helper()
	payload["cwd"] = cwd
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var errBuf bytes.Buffer
	getenv := func(string) string { return "" }
	err = runPreToolUse(bytes.NewReader(b), &errBuf, getenv, notFoundLookup, nil)
	if err == nil {
		return errBuf.String(), 0
	}
	type coder interface{ ExitCode() int }
	if c, ok := err.(coder); ok {
		return errBuf.String(), c.ExitCode()
	}
	t.Fatalf("unexpected error type %T: %v", err, err)
	return "", -1
}

func TestPreToolUse_ProtectedBareDeny(t *testing.T) {
	root := fakeWardRepoWithBody(t, protectedYAML)
	stderr, code := runHookInCwd(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gcloud auth login"},
	}, root)
	if code != 2 {
		t.Fatalf("want block (exit 2), got %d. stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "gcloud") {
		t.Errorf("stderr should name the binary: %q", stderr)
	}
}

func TestPreToolUse_ProtectedAbsolutePathDeny(t *testing.T) {
	root := fakeWardRepoWithBody(t, protectedYAML)
	stderr, code := runHookInCwd(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "/opt/homebrew/bin/gcloud auth login"},
	}, root)
	if code != 2 {
		t.Fatalf("want block (exit 2), got %d. stderr=%q", code, stderr)
	}
}

func TestPreToolUse_ProtectedRelativePathDeny(t *testing.T) {
	root := fakeWardRepoWithBody(t, protectedYAML)
	stderr, code := runHookInCwd(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "./bin/gcloud auth login"},
	}, root)
	if code != 2 {
		t.Fatalf("want block (exit 2), got %d. stderr=%q", code, stderr)
	}
}

func TestPreToolUse_NoConfigPassesThrough(t *testing.T) {
	root := t.TempDir() // no .ward / .coily marker
	stderr, code := runHookInCwd(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gcloud auth login"},
	}, root)
	if code != 0 {
		t.Fatalf("want pass-through (exit 0), got %d. stderr=%q", code, stderr)
	}
}

func TestPreToolUse_MalformedConfigPassesThrough(t *testing.T) {
	root := fakeWardRepoWithBody(t, "::: not yaml :::")
	stderr, code := runHookInCwd(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gcloud auth login"},
	}, root)
	if code != 0 {
		t.Fatalf("want pass-through on malformed config, got %d. stderr=%q", code, stderr)
	}
}
