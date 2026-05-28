package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

// runInstall is the test harness for runInstallHooks. Captures stdout
// into a buffer (via a tempfile, since runInstallHooks takes a *os.File).
func runInstall(t *testing.T, args installHooksArgs) (stdout string, exit int) {
	t.Helper()
	// Pipe stdout through a tempfile so we can read it back.
	tmp, err := os.CreateTemp(t.TempDir(), "stdout-*.txt")
	if err != nil {
		t.Fatalf("tempfile: %v", err)
	}
	defer tmp.Close()
	err = runInstallHooks(args, tmp)
	if _, seekErr := tmp.Seek(0, io.SeekStart); seekErr != nil {
		t.Fatalf("seek: %v", seekErr)
	}
	out, readErr := io.ReadAll(tmp)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}
	if err == nil {
		return string(out), 0
	}
	type coder interface{ ExitCode() int }
	if c, ok := err.(coder); ok {
		return string(out), c.ExitCode()
	}
	// Plain errors from urfave/cli's Run path become exit code 1.
	// Mirror that here so tests treat them as a generic failure.
	return string(out), 1
}

// hookPresent inspects a settings.json map and reports whether our
// hook entry is registered under hooks.PreToolUse[matcher=Bash].
func hookPresent(t *testing.T, m map[string]any) bool {
	t.Helper()
	hooks, ok := m["hooks"].(map[string]any)
	if !ok {
		return false
	}
	pre, ok := hooks["PreToolUse"].([]any)
	if !ok {
		return false
	}
	for _, raw := range pre {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if matcher, _ := entry["matcher"].(string); matcher != wantedMatcher {
			continue
		}
		inner, _ := entry["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); cmd == wantedCommand {
				return true
			}
		}
	}
	return false
}

func TestInstallHooks_FreshInstall(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")

	stdout, exit := runInstall(t, installHooksArgs{explicitPath: target})
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d (stdout: %s)", exit, stdout)
	}
	if !strings.Contains(stdout, "registered hook in") {
		t.Errorf("unexpected stdout: %s", stdout)
	}
	got := readJSON(t, target)
	if !hookPresent(t, got) {
		t.Fatalf("hook not present after fresh install: %v", got)
	}
}

func TestInstallHooks_PreservesUnrelatedKeys(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")
	writeJSON(t, target, map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Bash(ls:*)"},
			"deny":  []any{"Bash(rm:*)"},
		},
		"model": "claude-opus-4-7",
	})

	_, exit := runInstall(t, installHooksArgs{explicitPath: target})
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	got := readJSON(t, target)
	if !hookPresent(t, got) {
		t.Fatalf("hook not present")
	}
	perms, ok := got["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions key dropped")
	}
	if model, _ := got["model"].(string); model != "claude-opus-4-7" {
		t.Errorf("model key dropped or mutated: %v", got["model"])
	}
	if allow, _ := perms["allow"].([]any); len(allow) != 1 || allow[0] != "Bash(ls:*)" {
		t.Errorf("allow list mutated: %v", perms["allow"])
	}
}

func TestInstallHooks_Idempotent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")

	if _, exit := runInstall(t, installHooksArgs{explicitPath: target}); exit != 0 {
		t.Fatalf("first install failed")
	}
	stat1, _ := os.Stat(target)
	first := readJSON(t, target)

	stdout, exit := runInstall(t, installHooksArgs{explicitPath: target})
	if exit != 0 {
		t.Fatalf("second install failed: %s", stdout)
	}
	if !strings.Contains(stdout, "already registered") {
		t.Errorf("expected 'already registered' on second install, got: %s", stdout)
	}
	stat2, _ := os.Stat(target)
	if stat1.ModTime() != stat2.ModTime() {
		t.Errorf("file should not have been rewritten on idempotent re-install")
	}
	second := readJSON(t, target)
	if !equalJSON(first, second) {
		t.Errorf("second install changed content: first=%v second=%v", first, second)
	}
}

func TestInstallHooks_AppendsToExistingBashMatcher(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")
	writeJSON(t, target, map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "other-tool"},
					},
				},
			},
		},
	})

	_, exit := runInstall(t, installHooksArgs{explicitPath: target})
	if exit != 0 {
		t.Fatalf("install failed")
	}
	got := readJSON(t, target)
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("expected one Bash matcher entry, got %d: %v", len(pre), pre)
	}
	inner := pre[0].(map[string]any)["hooks"].([]any)
	if len(inner) != 2 {
		t.Fatalf("expected two hooks under the Bash matcher (other-tool + ours), got %d: %v", len(inner), inner)
	}
	if !hookPresent(t, got) {
		t.Fatalf("our hook missing")
	}
	// Verify the prior entry is preserved.
	found := false
	for _, h := range inner {
		hm := h.(map[string]any)
		if cmd, _ := hm["command"].(string); cmd == "other-tool" {
			found = true
		}
	}
	if !found {
		t.Errorf("pre-existing other-tool hook was dropped")
	}
}

func TestInstallHooks_AddsNewBashMatcherWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")
	writeJSON(t, target, map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Write",
					"hooks": []any{
						map[string]any{"type": "command", "command": "audit-writes"},
					},
				},
			},
		},
	})

	_, exit := runInstall(t, installHooksArgs{explicitPath: target})
	if exit != 0 {
		t.Fatalf("install failed")
	}
	got := readJSON(t, target)
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("expected two PreToolUse entries (Write + Bash), got %d: %v", len(pre), pre)
	}
	if !hookPresent(t, got) {
		t.Fatalf("our Bash hook missing")
	}
	// The pre-existing Write matcher should still be there.
	foundWrite := false
	for _, raw := range pre {
		entry := raw.(map[string]any)
		if matcher, _ := entry["matcher"].(string); matcher == "Write" {
			foundWrite = true
		}
	}
	if !foundWrite {
		t.Errorf("pre-existing Write matcher dropped")
	}
}

func TestInstallHooks_CheckMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")
	_, exit := runInstall(t, installHooksArgs{explicitPath: target, check: true})
	if exit != 1 {
		t.Fatalf("expected exit 1 on missing settings.json, got %d", exit)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("--check should not create the file")
	}
}

func TestInstallHooks_CheckPresent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")
	if _, exit := runInstall(t, installHooksArgs{explicitPath: target}); exit != 0 {
		t.Fatalf("install failed")
	}
	stat1, _ := os.Stat(target)
	stdout, exit := runInstall(t, installHooksArgs{explicitPath: target, check: true})
	if exit != 0 {
		t.Fatalf("expected exit 0 on present, got %d (stdout: %s)", exit, stdout)
	}
	stat2, _ := os.Stat(target)
	if stat1.ModTime() != stat2.ModTime() {
		t.Errorf("--check should not write the file")
	}
	if !strings.Contains(stdout, "hook present") {
		t.Errorf("unexpected stdout: %s", stdout)
	}
}

func TestInstallHooks_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")
	stdout, exit := runInstall(t, installHooksArgs{explicitPath: target, dryRun: true})
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	if !strings.Contains(stdout, "would write to") {
		t.Errorf("dry-run banner missing: %s", stdout)
	}
	if !strings.Contains(stdout, wantedCommand) {
		t.Errorf("dry-run output did not include the wanted command: %s", stdout)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry-run created the file")
	}
}

func TestInstallHooks_MalformedJSONErrors(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("not json {{{"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, exit := runInstall(t, installHooksArgs{explicitPath: target})
	if exit == 0 {
		t.Fatalf("expected non-zero exit on malformed settings.json")
	}
	// File should be unchanged.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "not json {{{" {
		t.Errorf("malformed settings.json was clobbered: %q", got)
	}
}

// equalJSON normalizes two map[string]any via JSON round-trip and
// compares. Sufficient for the on-disk shape we care about.
func equalJSON(a, b map[string]any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}
