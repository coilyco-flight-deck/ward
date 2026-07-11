package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCommandRegistered(t *testing.T) {
	if commandNamed(rootCommand().Commands, "setup") == nil {
		t.Fatalf("root command missing setup; got %v", commandNames(rootCommand().Commands))
	}
}

func TestRunSetupWithUnsetRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "")
	t.Setenv("WARD_TARGET_OWNER", "coilysiren")
	t.Setenv("WARD_TARGET_REPO", "coilysiren/example")
	report, err := runSetup(context.Background())
	if err != nil {
		t.Fatalf("runSetup with unset ref: %v", err)
	}
	if !strings.Contains(report.sourceSummary, "no external config source active") {
		t.Errorf("source summary = %q, want the baked-source note", report.sourceSummary)
	}
	if report.resolvedSHA != "embedded" {
		t.Errorf("resolved SHA = %q, want embedded", report.resolvedSHA)
	}
	if report.cachePath != "embedded neutral default" {
		t.Errorf("cache path = %q, want the embedded default marker", report.cachePath)
	}
	if got := strings.Join(report.validatedSurfaces, ", "); got != setupValidatedSurfaces {
		t.Errorf("validated surfaces = %q, want %q", got, setupValidatedSurfaces)
	}
}

func TestRunSetupRejectsMalformedRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	if _, err := runSetup(context.Background()); err == nil {
		t.Fatal("runSetup with malformed ref: want a loud config-source error")
	} else if !strings.Contains(err.Error(), wardConfigRefEnv) {
		t.Errorf("error %q does not name %s", err, wardConfigRefEnv)
	}
}

func TestRunSetupWithFixtureRef(t *testing.T) {
	dir := writeBundleFixture(t)
	gitFixture(t, dir, "init", "-b", "main", ".")
	gitFixture(t, dir, "add", ".")
	gitFixture(t, dir, "commit", "-m", "bundle")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs(%s): %v", dir, err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+abs)
	report, err := runSetup(context.Background())
	if err != nil {
		t.Fatalf("runSetup with fixture ref: %v", err)
	}
	if !strings.HasPrefix(report.sourceSummary, "WARD_CONFIG_REF=file://") {
		t.Errorf("source summary = %q, want the file fixture ref", report.sourceSummary)
	}
	if report.resolvedSHA == "" || report.resolvedSHA == "embedded" || report.resolvedSHA == "unavailable" {
		t.Errorf("resolved SHA = %q, want the fixture checkout HEAD", report.resolvedSHA)
	}
	if report.cachePath != abs {
		t.Errorf("cache path = %q, want %q", report.cachePath, abs)
	}
	if got := strings.Join(report.validatedSurfaces, ", "); got != setupValidatedSurfaces {
		t.Errorf("validated surfaces = %q, want %q", got, setupValidatedSurfaces)
	}
}
