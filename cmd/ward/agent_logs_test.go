package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAgentLogsSourceFallsBackToDispatchLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1033}
	dispatchDir := filepath.Join(agentLogsDir(), dispatchLogsSubdir)
	if err := os.MkdirAll(dispatchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dispatch logs: %v", err)
	}
	logPath := filepath.Join(dispatchDir, "20260710T101500Z-director-box-coilyco-flight-deck-ward-1033.log")
	if err := os.WriteFile(logPath, []byte(
		"ward dispatch broker: director-box requested `ward agent engineer coilyco-flight-deck/ward#1033 --harness codex`\n"+
			"WARD-DISPATCH: failed ❌\n"+
			"Failure: `pre-flight NO-GO`\n"), 0o644); err != nil {
		t.Fatalf("write dispatch log: %v", err)
	}

	r := fakeEngineerVisibilityDockerRunner(t, "", 0)
	source, err := r.resolveAgentLogsSourceForIssue(t.Context(), ref, 0, false)
	if err != nil {
		t.Fatalf("resolveAgentLogsSourceForIssue: %v", err)
	}
	if source.Kind != agentLogSourceFile {
		t.Fatalf("source kind = %q, want %q", source.Kind, agentLogSourceFile)
	}
	if source.Path != logPath {
		t.Fatalf("source path = %q, want %q", source.Path, logPath)
	}
	body, err := os.ReadFile(source.Path)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("dispatch log fallback returned an empty file")
	}
}
