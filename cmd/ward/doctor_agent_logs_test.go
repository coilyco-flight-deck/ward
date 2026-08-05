package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorWarnsAboutHistoricalRawAgentArchives(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	rawDir := historicalRawAgentLogsDir()
	if err := os.MkdirAll(filepath.Join(rawDir, "engineer-codex-ward-1582"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "engineer-codex-ward-1582", "console.log"), []byte("historical"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, _ := runDoctor(t.Context())
	if len(report.warnings) != 1 || !strings.Contains(report.warnings[0], rawDir) {
		t.Fatalf("doctor warnings = %q, want exact historical raw path %q", report.warnings, rawDir)
	}
}
