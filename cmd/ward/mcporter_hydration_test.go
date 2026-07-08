package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHydrateMcporterFromWritesHomeConfig(t *testing.T) {
	home := t.TempDir()
	script := filepath.Join(t.TempDir(), "merge-mcporter.py")
	if err := os.WriteFile(script, []byte(strings.TrimSpace(`
#!/usr/bin/env python3
from pathlib import Path
import json
import os

target = Path.home() / ".mcporter" / "mcporter.json"
target.parent.mkdir(parents=True, exist_ok=True)
target.write_text(json.dumps({
    "home": os.environ["HOME"],
    "projects": os.environ["COILYSIREN_PROJECTS"],
}))
`)+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	if err := (&Runner{}).hydrateMcporterFrom(context.Background(), home, script); err != nil {
		t.Fatalf("hydrateMcporterFrom: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".mcporter", "mcporter.json"))
	if err != nil {
		t.Fatalf("read hydrated config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, home) {
		t.Fatalf("hydrated config = %s, want HOME %q", got, home)
	}
	if !strings.Contains(got, "/workspace") {
		t.Fatalf("hydrated config = %s, want COILYSIREN_PROJECTS=/workspace", got)
	}
}
