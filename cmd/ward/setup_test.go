package main

import (
	"context"
	"os"
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
	t.Setenv("WARD_TARGET_OWNER", "")
	t.Setenv("WARD_TARGET_REPO", "")
	t.Setenv("WARD_READONLY", "")
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

func TestRunSetupAllowsOptionalPlaceholderValues(t *testing.T) {
	dir := writeBundleFixture(t)
	agentsPath := filepath.Join(dir, bundleFixtureAgentsPath)
	b, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentsPath, err)
	}
	if !strings.Contains(string(b), "example-bot") || !strings.Contains(string(b), "bot@example.com") {
		t.Fatalf("fixture lost the optional placeholder attribution:\n%s", b)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	report, err := runSetup(context.Background())
	if err != nil {
		t.Fatalf("runSetup with optional placeholder values: %v; checks=%+v", err, report)
	}
}

func TestRunSetupRejectsRequiredOpsPlaceholderSentinel(t *testing.T) {
	dir := writeBundleFixture(t)
	writeBundleFixtureFile(t, dir, bundleFixtureForgejoPath, `
wrap ward-kdl ops forgejo {
    spec golf.json
    base-url (placeholder)"git.example.com/api/v1"
    auth header-token {
        header Authorization
        prefix "token "
        value ssm "/coilyco/forgejo/api-token"
    }
    restrict owner matches coilyco*
    can get repo
}
`)
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	report, err := runSetup(context.Background())
	if err == nil {
		t.Fatalf("runSetup with required placeholder sentinel passed; report=%+v", report)
	}
	for _, want := range []string{
		"setup surface compile: ops",
		bundleFixtureForgejoPath,
		"placeholder sentinel survived at wrap > base-url",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestRunSetupWithRelativeFileRef(t *testing.T) {
	file := writeSingleFileBundleFixture(t)
	rel, err := filepath.Rel(resolveInvokeCWD(), file)
	if err != nil {
		t.Fatalf("rel(%s): %v", file, err)
	}
	t.Setenv(wardConfigRefEnv, rel)
	report, err := runSetup(context.Background())
	if err != nil {
		t.Fatalf("runSetup with relative file ref: %v", err)
	}
	if !strings.Contains(report.sourceSummary, "WARD_CONFIG_REF="+rel) {
		t.Errorf("source summary = %q, want the relative file ref", report.sourceSummary)
	}
	if report.cachePath != file {
		t.Errorf("cache path = %q, want %q", report.cachePath, file)
	}
}
