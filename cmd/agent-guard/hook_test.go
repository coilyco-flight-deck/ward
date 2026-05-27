package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runHook is the test harness for runPreToolUse. Returns (stderr, exitCode).
// 0 = pass-through, 2 = block. Default lookup is ENOENT (skips path check).
func runHook(t *testing.T, payload map[string]interface{}, env map[string]string) (string, int) {
	t.Helper()
	return runHookWithLookup(t, payload, env, notFoundLookup)
}

func runHookWithLookup(t *testing.T, payload map[string]interface{}, env map[string]string, lookup pathLookup) (string, int) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var errBuf bytes.Buffer
	getenv := func(k string) string { return env[k] }
	err = runPreToolUse(bytes.NewReader(b), &errBuf, getenv, lookup, nil)
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

// runHookWithCheck mirrors runHook but injects a registryCheck.
func runHookWithCheck(t *testing.T, payload map[string]interface{}, check registryCheck) (string, int) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var errBuf bytes.Buffer
	getenv := func(string) string { return "" }
	err = runPreToolUse(bytes.NewReader(b), &errBuf, getenv, notFoundLookup, check)
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

// notFoundLookup pretends every binary is absent from PATH. ENOENT skips path check.
func notFoundLookup(string) (string, error) {
	return "", exec.ErrNotFound
}

// staticLookup returns the given resolved path for any binary name.
// For tests that want to assert path-check behavior.
func staticLookup(resolved string) pathLookup {
	return func(string) (string, error) { return resolved, nil }
}

// fakeRepo writes a marker file so detectGuard sees the desired guard
// name when called with the returned cwd.
func fakeRepo(t *testing.T, marker string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.Dir(marker))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, marker), []byte("commands: {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

func TestPreToolUse_NonBashPassesThrough(t *testing.T) {
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "/etc/passwd"},
	}, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("expected pass-through, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_EmptyCommandPassesThrough(t *testing.T) {
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "   "},
	}, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("expected pass-through, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_CoilyRepoBlocksBareGh(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gh issue view 506 --repo coilysiren/agentic-os-kai"},
		"cwd":        cwd,
	}, nil)
	if code != 2 {
		t.Fatalf("expected block (exit 2), got %d", code)
	}
	if !strings.Contains(stderr, "coily ops gh") {
		t.Errorf("expected recovery hint to mention `coily ops gh`, got: %s", stderr)
	}
	if !strings.Contains(stderr, "GraphQL") {
		t.Errorf("expected GraphQL trap hint for `gh issue view`, got: %s", stderr)
	}
}

func TestPreToolUse_GhApiDoesNotTripGraphQLHint(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gh api /repos/coilysiren/agentic-os-kai/issues/506"},
		"cwd":        cwd,
	}, nil)
	if code != 2 {
		t.Fatalf("expected block (exit 2), got %d", code)
	}
	if !strings.Contains(stderr, "coily ops gh") {
		t.Errorf("expected recovery hint, got: %s", stderr)
	}
	if strings.Contains(stderr, "GraphQL") {
		t.Errorf("`gh api ...` is REST; should not mention GraphQL trap. got: %s", stderr)
	}
}

func TestPreToolUse_CoilyRepoBlocksBareKubectl(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "kubectl get pods"},
		"cwd":        cwd,
	}, nil)
	if code != 2 || !strings.Contains(stderr, "coily ops kubectl") {
		t.Fatalf("expected `coily ops kubectl` hint, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_AgentGuardRepoBlocksBareMake(t *testing.T) {
	cwd := fakeRepo(t, ".agent-guard/agent-guard.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "make test"},
		"cwd":        cwd,
	}, nil)
	if code != 2 || !strings.Contains(stderr, "agent-guard exec") {
		t.Fatalf("expected `agent-guard exec` hint, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_AgentGuardRepoDoesNotBlockGh(t *testing.T) {
	// gh has no agent-guard wrapper. Pass through; permissions.deny handles deny.
	cwd := fakeRepo(t, ".agent-guard/agent-guard.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gh issue view 1"},
		"cwd":        cwd,
	}, nil)
	if code != 0 {
		t.Fatalf("expected pass-through, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_EnvPrefixStripped(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "env FOO=bar BAZ=qux gh issue view 1"},
		"cwd":        cwd,
	}, nil)
	if code != 2 || !strings.Contains(stderr, "coily ops gh") {
		t.Fatalf("expected env-prefix-stripped gh hint, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_SudoPrefixStripped(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "sudo kubectl get nodes"},
		"cwd":        cwd,
	}, nil)
	if code != 2 || !strings.Contains(stderr, "coily ops kubectl") {
		t.Fatalf("expected sudo-stripped kubectl hint, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_PipelineFlagsFirstSegmentHit(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gh issue list | grep foo"},
		"cwd":        cwd,
	}, nil)
	if code != 2 || !strings.Contains(stderr, "coily ops gh") {
		t.Fatalf("expected first-segment gh hint, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_CommandSubstitutionInspected(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": `echo $(aws sts get-caller-identity)`},
		"cwd":        cwd,
	}, nil)
	if code != 2 || !strings.Contains(stderr, "coily ops aws") {
		t.Fatalf("expected aws inside $() to be caught, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_UnknownTokenPassesThrough(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "ls -la /tmp"},
		"cwd":        cwd,
	}, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("expected pass-through, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_UnparseableJSONPassesThrough(t *testing.T) {
	var errBuf bytes.Buffer
	err := runPreToolUse(strings.NewReader("not json"), &errBuf, func(string) string { return "" }, notFoundLookup, nil)
	if err != nil || errBuf.Len() != 0 {
		t.Fatalf("expected pass-through, got err=%v stderr=%q", err, errBuf.String())
	}
}

func TestPreToolUse_NoCwdFallsBackToPWD(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gh issue view 1"},
	}, map[string]string{"PWD": cwd})
	if code != 2 || !strings.Contains(stderr, "coily ops gh") {
		t.Fatalf("expected PWD-fallback to detect coily guard, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_NoGuardMarkerDefaultsToAgentGuardTable(t *testing.T) {
	cwd := t.TempDir()
	// No marker: agent-guard table applies. make is in it; gh is not.
	stderrMake, codeMake := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "make build"},
		"cwd":        cwd,
	}, nil)
	if codeMake != 2 || !strings.Contains(stderrMake, "agent-guard exec") {
		t.Fatalf("expected agent-guard exec hint for make, got code=%d stderr=%q", codeMake, stderrMake)
	}
	stderrGh, codeGh := runHook(t, map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "gh issue view 1"},
		"cwd":        cwd,
	}, nil)
	if codeGh != 0 {
		t.Fatalf("expected gh to pass through under agent-guard default, got code=%d stderr=%q", codeGh, stderrGh)
	}
}

func TestPathCheck_CoilyOnCanonicalPathPassesThrough(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	// Canonical path skips path-check; coily is not in any routing table, so hook passes.
	stderr, code := runHookWithLookup(t,
		map[string]interface{}{
			"tool_name":  "Bash",
			"tool_input": map[string]interface{}{"command": "coily ops gh api /repos/x/y/issues/1"},
			"cwd":        cwd,
		},
		nil,
		staticLookup("/opt/homebrew/bin/coily"),
	)
	if code != 0 {
		t.Fatalf("expected pass-through for canonical coily, got code=%d stderr=%q", code, stderr)
	}
}

func TestPathCheck_CoilyOffCanonicalPathBlocks(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHookWithLookup(t,
		map[string]interface{}{
			"tool_name":  "Bash",
			"tool_input": map[string]interface{}{"command": "coily ops gh api /repos/x/y"},
			"cwd":        cwd,
		},
		nil,
		staticLookup("/tmp/evil/coily"),
	)
	if code != 2 {
		t.Fatalf("expected block on off-path coily, got code=%d", code)
	}
	if !strings.Contains(stderr, "/tmp/evil/coily") {
		t.Errorf("expected hijack message to name the offending path, got: %s", stderr)
	}
	if !strings.Contains(stderr, "PATH-hijack") {
		t.Errorf("expected hijack wording, got: %s", stderr)
	}
}

func TestPathCheck_AgentGuardOffCanonicalPathBlocks(t *testing.T) {
	cwd := fakeRepo(t, ".agent-guard/agent-guard.yaml")
	stderr, code := runHookWithLookup(t,
		map[string]interface{}{
			"tool_name":  "Bash",
			"tool_input": map[string]interface{}{"command": "agent-guard exec build"},
			"cwd":        cwd,
		},
		nil,
		staticLookup("/Users/kai/go/bin/agent-guard"),
	)
	if code != 2 {
		t.Fatalf("expected block on off-path agent-guard, got code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "/Users/kai/go/bin/agent-guard") {
		t.Errorf("expected hijack message to name the offending path, got: %s", stderr)
	}
}

func TestPathCheck_BinaryNotFoundSkipsCheck(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	// ENOENT -> skip path-check. coily has no routing-hint entry, so hook passes through.
	stderr, code := runHookWithLookup(t,
		map[string]interface{}{
			"tool_name":  "Bash",
			"tool_input": map[string]interface{}{"command": "coily ops aws s3 ls"},
			"cwd":        cwd,
		},
		nil,
		notFoundLookup,
	)
	if code != 0 {
		t.Fatalf("expected pass-through when coily not on PATH, got code=%d stderr=%q", code, stderr)
	}
}

func TestPathCheck_OtherLookupErrorBlocks(t *testing.T) {
	cwd := fakeRepo(t, ".coily/coily.yaml")
	lookup := func(string) (string, error) {
		return "", os.ErrPermission
	}
	stderr, code := runHookWithLookup(t,
		map[string]interface{}{
			"tool_name":  "Bash",
			"tool_input": map[string]interface{}{"command": "coily exec test"},
			"cwd":        cwd,
		},
		nil,
		lookup,
	)
	if code != 2 {
		t.Fatalf("expected block on unexpected lookup error, got code=%d", code)
	}
	if !strings.Contains(stderr, "Resolution of `coily` failed") {
		t.Errorf("expected resolution-failed message, got: %s", stderr)
	}
}

func TestPathCheck_FiresBeforeRoutingHint(t *testing.T) {
	// Off-canonical guard binary blocks first with hijack message, before routing hint.
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHookWithLookup(t,
		map[string]interface{}{
			"tool_name":  "Bash",
			"tool_input": map[string]interface{}{"command": "coily && gh issue view 1"},
			"cwd":        cwd,
		},
		nil,
		staticLookup("/tmp/coily"),
	)
	if code != 2 {
		t.Fatalf("expected block, got code=%d", code)
	}
	if !strings.Contains(stderr, "PATH-hijack") {
		t.Errorf("expected hijack message to win over routing hint, got: %s", stderr)
	}
	if strings.Contains(stderr, "coily ops gh") {
		t.Errorf("routing hint leaked when hijack should have been authoritative: %s", stderr)
	}
}

func TestPathCheck_NonGuardBinaryIgnored(t *testing.T) {
	// gh is not a guard binary; path-check skipped. Only routing-hint applies.
	cwd := fakeRepo(t, ".coily/coily.yaml")
	stderr, code := runHookWithLookup(t,
		map[string]interface{}{
			"tool_name":  "Bash",
			"tool_input": map[string]interface{}{"command": "gh issue view 1"},
			"cwd":        cwd,
		},
		nil,
		staticLookup("/tmp/gh"),
	)
	if code != 2 || !strings.Contains(stderr, "coily ops gh") {
		t.Fatalf("expected routing hint (not hijack) for gh, got code=%d stderr=%q", code, stderr)
	}
}

// stubCheck is a registryCheck returning the same fixed message for any path.
func stubCheck(msg string) registryCheck {
	return func(string) (string, error) { return msg, nil }
}

func TestPreToolUse_EditWithNoConflictPassesThrough(t *testing.T) {
	stderr, code := runHookWithCheck(t, map[string]interface{}{
		"tool_name":  "Edit",
		"tool_input": map[string]interface{}{"file_path": "/work/foo.md"},
	}, stubCheck(""))
	if code != 0 || stderr != "" {
		t.Fatalf("expected pass-through, got code=%d stderr=%q", code, stderr)
	}
}

func TestPreToolUse_EditWithConflictBlocks(t *testing.T) {
	stderr, code := runHookWithCheck(t, map[string]interface{}{
		"tool_name":  "Edit",
		"tool_input": map[string]interface{}{"file_path": "/work/foo.md"},
	}, stubCheck("pid=42 ref=coilysiren/coily#119 claim=/work reason=ancestor\n"))
	if code != 2 {
		t.Fatalf("expected block (exit 2), got code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "pid=42") || !strings.Contains(stderr, "dispatch registry list") {
		t.Fatalf("expected holder info + recovery hint in stderr, got %q", stderr)
	}
}

func TestPreToolUse_WriteWithConflictBlocks(t *testing.T) {
	_, code := runHookWithCheck(t, map[string]interface{}{
		"tool_name":  "Write",
		"tool_input": map[string]interface{}{"file_path": "/work/x.go"},
	}, stubCheck("pid=1 ref=r claim=/work reason=ancestor\n"))
	if code != 2 {
		t.Fatalf("expected Write to block, got code=%d", code)
	}
}

func TestPreToolUse_MultiEditWithConflictBlocks(t *testing.T) {
	_, code := runHookWithCheck(t, map[string]interface{}{
		"tool_name":  "MultiEdit",
		"tool_input": map[string]interface{}{"file_path": "/work/x.go"},
	}, stubCheck("pid=1 ref=r claim=/work reason=ancestor\n"))
	if code != 2 {
		t.Fatalf("expected MultiEdit to block, got code=%d", code)
	}
}

func TestPreToolUse_NotebookEditUsesNotebookPath(t *testing.T) {
	_, code := runHookWithCheck(t, map[string]interface{}{
		"tool_name":  "NotebookEdit",
		"tool_input": map[string]interface{}{"notebook_path": "/work/x.ipynb"},
	}, stubCheck("pid=1 ref=r claim=/work reason=ancestor\n"))
	if code != 2 {
		t.Fatalf("expected NotebookEdit to block via notebook_path, got code=%d", code)
	}
}

func TestPreToolUse_EditWithRelativePathPassesThrough(t *testing.T) {
	called := false
	check := registryCheck(func(string) (string, error) {
		called = true
		return "should-not-fire", nil
	})
	stderr, code := runHookWithCheck(t, map[string]interface{}{
		"tool_name":  "Edit",
		"tool_input": map[string]interface{}{"file_path": "relative/path.md"},
	}, check)
	if called {
		t.Fatalf("registry check fired for relative path; expected skip")
	}
	if code != 0 || stderr != "" {
		t.Fatalf("expected pass-through for relative path, got code=%d stderr=%q", code, stderr)
	}
}

func TestDefaultRegistryCheck_EnvUnsetReturnsEmpty(t *testing.T) {
	t.Setenv("CLI_GUARD_DISPATCH_LOG_ROOT", "")
	msg, err := defaultRegistryCheck("/work/foo")
	if err != nil || msg != "" {
		t.Fatalf("unset env: want (\"\", nil), got (%q, %v)", msg, err)
	}
}

func TestPreToolUse_EditWithNilCheckPassesThrough(t *testing.T) {
	stderr, code := runHookWithCheck(t, map[string]interface{}{
		"tool_name":  "Edit",
		"tool_input": map[string]interface{}{"file_path": "/work/foo.md"},
	}, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("nil check should pass through, got code=%d stderr=%q", code, stderr)
	}
}
