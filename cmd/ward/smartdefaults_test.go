package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSmartDefaultsBaked(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "")
	t.Setenv("WARD_TARGET_OWNER", "")
	t.Setenv("WARD_TARGET_REPO", "")
	t.Setenv("WARD_READONLY", "")
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("currentSmartDefaultsWithError(baked): %v", err)
	}
	want := canonicalSmartDefaults(t)
	if !reflect.DeepEqual(defs, want) {
		t.Fatalf("baked smart defaults no longer match the canonical defaults source\nwant: %#v\ngot:  %#v", want, defs)
	}
}

func TestSmartDefaultsFromBundleSource(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-reservation-ttl "2h"
    agent-reservation-recheck-max "9s"
    agent-reap-idle "90m"
    agent-reap-max-cpu "7.5"
    agent-image "ghcr.io/example/ward-agent"
    agent-tag "2026.07"
    container-memory-limit "3g"
    engineer-container-limit "17"
    engineer-repo-working-limit "3"
    engineer-open-pr-branch-limit "8"
    director-max-parallel "13"
    director-limit "77"
    director-poll-interval "45s"
    reviewer-timeout "11m"
    config-bundle-ttl "900"
    container-assets-ttl "3h"
    container-read-only-extra-repo-ttl "48h"
    container-reap-ttl "48h"
    agent-workflow default="merge-remote-main" {
    }
    pr-merge-style "squash"
}
`
	reposBody := `repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
    }
    burndown default=#true {
        repo "coilyco-flight-deck/infrastructure" #false
        repo "coilyco-gaming/eco-ops" #false
    }
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(reposBody), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	defs, err := loadSmartDefaultsFrom(bundleConfigSource(dir))
	if err != nil {
		t.Fatalf("loadSmartDefaultsFrom(bundle source): %v", err)
	}
	if defs.agentReservationTTL != 2*time.Hour || defs.reservationRecheckDefaultMax != 9*time.Second {
		t.Errorf("bundle reservation defaults = %+v", defs)
	}
	if defs.agentReapIdleDefault != 90*time.Minute || defs.agentReapMaxCPUDefault != 7.5 {
		t.Errorf("bundle reap defaults = %+v", defs)
	}
	if defs.agentImage != "ghcr.io/example/ward-agent" || defs.agentTag != "2026.07" {
		t.Errorf("bundle agent image/tag = %q:%q", defs.agentImage, defs.agentTag)
	}
	if defs.containerMemoryLimit != "3g" {
		t.Errorf("bundle container memory limit = %q, want 3g", defs.containerMemoryLimit)
	}
	if defs.engineerContainerLimit != 17 || defs.engineerRepoWorkingLimit != 3 || defs.directorMaxParallel != 13 || defs.directorLimit != 77 || defs.directorPollInterval != 45*time.Second {
		t.Errorf("bundle director defaults = %+v", defs)
	}
	if defs.engineerOpenPRBranchLimit != 8 {
		t.Errorf("bundle open-pr defaults = %+v", defs)
	}
	if defs.reviewerTimeout != 11*time.Minute || defs.configBundleTTL != 15*time.Minute {
		t.Errorf("bundle duration defaults = %+v", defs)
	}
	if defs.containerAssetsTTL != 3*time.Hour || defs.containerReadOnlyExtraRepoTTL != 48*time.Hour || defs.containerReapTTL != 48*time.Hour {
		t.Errorf("bundle container defaults = %+v", defs)
	}
	if defs.agentWorkflowDefault != workflowDirectToMain {
		t.Errorf("bundle workflow default = %q, want merge-remote-main", defs.agentWorkflowDefault)
	}
	if defs.prMergeStyle != "squash" {
		t.Errorf("bundle pr merge style = %q, want squash", defs.prMergeStyle)
	}
	if len(defs.agentWorkflowRepos) != 0 {
		t.Errorf("bundle workflow overrides = %v, want none in the neutral starter", defs.agentWorkflowRepos)
	}
	if !defs.burndownEnabled("coilyco-flight-deck/ward") {
		t.Error("bundle burndown default should leave ordinary repos eligible")
	}
	if defs.burndownEnabled("coilyco-flight-deck/infrastructure") || defs.burndownEnabled("coilyco-gaming/eco-ops") {
		t.Errorf("bundle burndown exclusions = infra:%t eco-ops:%t, want both false", defs.burndownEnabled("coilyco-flight-deck/infrastructure"), defs.burndownEnabled("coilyco-gaming/eco-ops"))
	}
}

func TestSmartDefaultsIgnoresBadOperatorConfigRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	got, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("currentSmartDefaultsWithError with bad operator ref: %v", err)
	}
	if want := canonicalSmartDefaults(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("currentSmartDefaultsWithError() = %#v, want baked %#v", got, want)
	}
}

func TestSmartDefaultsRejectsMalformedValue(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-reservation-ttl "nope"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write malformed defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
    }
}`), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(dir)); err == nil {
		t.Fatal("malformed smart defaults selected a bundle; want a loud parse error")
	}
}

func TestSmartDefaultsRejectsReservationTTLUnderRoleLimit(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-reservation-ttl "1h"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner "example-owner"
        repo "example-owner/*" forge=github
    }
}`), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(dir)); err == nil || !strings.Contains(err.Error(), "must exceed role") {
		t.Fatalf("reservation ttl under role limit should fail loud, got %v", err)
	}
}

func TestSmartDefaultsBundleMissingFileFailsLoud(t *testing.T) {
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(t.TempDir())); err == nil {
		t.Fatal("bundle without smart defaults selected a source; want a loud read error")
	}
}

func TestSmartDefaultsRejectsInvalidWorkflow(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    agent-workflow default="merge-it"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write malformed defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
    }
}`), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(dir)); err == nil {
		t.Fatal("invalid workflow default selected a bundle; want a loud parse error")
	}
}

func TestSmartDefaultsRejectsInvalidMergeStyle(t *testing.T) {
	dir := t.TempDir()
	defaultsBody := `defaults {
    pr-merge-style "manual-rocket"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write malformed defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
    }
}`), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(dir)); err == nil {
		t.Fatal("invalid pr merge style selected a bundle; want a loud parse error")
	}
}

func TestSmartDefaultsRejectsMissingRepoAuthority(t *testing.T) {
	dir := t.TempDir()
	body := `defaults {
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
}`), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(dir)); err == nil {
		t.Fatal("bundle without repo-authority selected a source; want a loud parse error")
	}
}

func TestSmartDefaultsIgnoreMalformedOperatorBundle(t *testing.T) {
	dir := t.TempDir()
	// 1h undercuts the built-in engineer 90m limit: trips the TTL invariant.
	if err := os.WriteFile(filepath.Join(dir, "workflow.kdl"),
		[]byte("defaults {\n    agent-reservation-ttl \"1h\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repos.kdl"),
		[]byte("repos {\n    repo-authority default=forgejo {\n        trusted-owner coilysiren\n    }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "file://" + filepath.ToSlash(dir)
	t.Setenv(wardConfigRefEnv, ref)

	got, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("currentSmartDefaultsWithError with malformed operator bundle: %v", err)
	}
	if want := canonicalSmartDefaults(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("currentSmartDefaultsWithError() = %#v, want baked %#v (operator ref %s)", got, want, ref)
	}
}
