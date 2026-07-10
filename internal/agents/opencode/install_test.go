package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

type recordingExec struct {
	calls [][]string
}

func (r *recordingExec) Exec(_ context.Context, bin string, argv ...string) error {
	call := append([]string{bin}, argv...)
	r.calls = append(r.calls, call)
	return nil
}

func (r *recordingExec) Capture(context.Context, string, ...string) ([]byte, error) { return nil, nil }

// TestInstallFailsLoudlyWhenBinaryStaysMissing proves opencode does not silently
// continue when the required install does not make the harness binary available.
func TestInstallFailsLoudlyWhenBinaryStaysMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	exec := &recordingExec{}
	rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: home, Log: noopLog, Exec: exec}
	err := (Agent{}).Install(rc)
	if err == nil {
		t.Fatal("expected opencode Install to fail when the binary remains missing")
	}
	for _, want := range []string{"opencode", "PATH"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Install error %q missing %q", err.Error(), want)
		}
	}
	if len(exec.calls) == 0 {
		t.Fatal("opencode Install did not attempt the bootstrap command")
	}
	if got := exec.calls[0][0]; got != "bash" {
		t.Fatalf("first install command = %q, want bash", got)
	}
}

// TestInstallNoOpWhenBinaryAlreadyPresent proves the required install hook
// short-circuits when opencode is already on PATH.
func TestInstallNoOpWhenBinaryAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, record.Binary), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	rc := agentsapi.RunCtx{Ctx: context.Background(), AgentHome: t.TempDir(), Log: noopLog, Exec: &recordingExec{}}
	if err := (Agent{}).Install(rc); err != nil {
		t.Fatalf("Install with an existing binary should no-op, got %v", err)
	}
}
