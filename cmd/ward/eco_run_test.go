package main

import (
	"runtime"
	"strings"
	"testing"
)

// ecoBootCommand points the server at the throwaway world and runs headless.
func TestEcoBootCommand(t *testing.T) {
	exe, argv, env := ecoBootCommand("/opt/EcoServer", "/tmp/world", []string{"EcoTelemetry"})

	wantExe := "EcoServer"
	if runtime.GOOS == "windows" {
		wantExe = "EcoServer.exe"
	}
	if !strings.HasSuffix(exe, wantExe) {
		t.Fatalf("exe = %q, want suffix %q", exe, wantExe)
	}
	if !contains(argv, "-nogui") {
		t.Fatalf("argv %v missing -nogui (headless)", argv)
	}
	if !contains(argv, "-storage=/tmp/world") {
		t.Fatalf("argv %v missing throwaway -storage", argv)
	}
	if !anyHasPrefix(env, "ECO_MODS=EcoTelemetry") {
		t.Fatalf("env %v missing ECO_MODS", env)
	}
}

// scanReady matches a ready marker anywhere in the captured log.
func TestScanReady(t *testing.T) {
	sig := defaultSmokeSignals()
	if !scanReady("...\nGame world is ready\n", sig.readyMarkers) {
		t.Fatalf("should detect ready marker")
	}
	if scanReady("still booting\n", sig.readyMarkers) {
		t.Fatalf("should not detect ready before the marker")
	}
}

// lastNonEmptyLine returns the trailing non-blank line (the snapshot id).
func TestLastNonEmptyLine(t *testing.T) {
	if got := lastNonEmptyLine(">>> snapshotting\nsnap-2026-07-03T12-00-00\n\n"); got != "snap-2026-07-03T12-00-00" {
		t.Fatalf("lastNonEmptyLine = %q", got)
	}
	if got := lastNonEmptyLine("   \n\n"); got != "" {
		t.Fatalf("all-blank should be empty, got %q", got)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func anyHasPrefix(xs []string, p string) bool {
	for _, x := range xs {
		if strings.HasPrefix(x, p) {
			return true
		}
	}
	return false
}
