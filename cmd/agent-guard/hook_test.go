package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runHook is the test harness for runPreToolUse. Returns (stderr, exitCode).
// exitCode 0 means pass-through; 2 means block.
func runHook(t *testing.T, payload map[string]interface{}, env map[string]string) (string, int) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var errBuf bytes.Buffer
	getenv := func(k string) string { return env[k] }
	err = runPreToolUse(bytes.NewReader(b), &errBuf, getenv)
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
		"tool_input": map[string]interface{}{"command": "gh issue view 506 --repo coilysiren/coilyco-ai"},
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
		"tool_input": map[string]interface{}{"command": "gh api /repos/coilysiren/coilyco-ai/issues/506"},
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
	// gh has no agent-guard wrapper. v0 routing table passes it through;
	// any deny is the responsibility of permissions.deny in the consumer
	// repo's settings.json.
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
	err := runPreToolUse(strings.NewReader("not json"), &errBuf, func(string) string { return "" })
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
	// In a directory with no marker: agent-guard table applies. `make` is
	// in the agent-guard table, gh is not.
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
