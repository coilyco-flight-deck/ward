package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSmartDefaultsBaked(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "")
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("currentSmartDefaultsWithError(baked): %v", err)
	}
	if defs.agentReservationTTL != time.Hour {
		t.Errorf("baked reservation ttl = %s, want 1h", defs.agentReservationTTL)
	}
	if defs.directorMaxParallel != 10 || defs.directorLimit != 50 || defs.containerReapKeep != 10 {
		t.Errorf("baked defaults = %+v, want the neutral policy bundle", defs)
	}
}

func TestSmartDefaultsFromBundleSource(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-reservation-ttl "2h"
    agent-reservation-recheck-max "9s"
    agent-reap-idle "90m"
    agent-reap-max-cpu "7.5"
    director-max-parallel "13"
    director-limit "77"
    director-poll-interval "45s"
    reviewer-timeout "11m"
    config-bundle-ttl "900"
    container-assets-ttl "3h"
    container-read-only-extra-repo-ttl "48h"
    container-reap-keep "12"
    agent-workflow default="direct-main" {
        repo "coilyco-flight-deck/ward" workflow="pull-requests-and-merge"
    }
}

repo-authority default=forgejo {
    trusted-owner "coilysiren"
    trusted-owner "coilyco-bridge"
    trusted-owner "coilyco-flight-deck"
    trusted-owner "coilyco-gaming"

    repo "coilysiren/*" forge=github
    repo "coilyco-bridge/*" forge=forgejo
    repo "coilyco-flight-deck/*" forge=forgejo
    repo "coilyco-gaming/*" forge=forgejo
}`
	if err := os.WriteFile(filepath.Join(dir, bundleDefaultsKDLPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
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
	if defs.directorMaxParallel != 13 || defs.directorLimit != 77 || defs.directorPollInterval != 45*time.Second {
		t.Errorf("bundle director defaults = %+v", defs)
	}
	if defs.reviewerTimeout != 11*time.Minute || defs.configBundleTTL != 15*time.Minute {
		t.Errorf("bundle duration defaults = %+v", defs)
	}
	if defs.containerAssetsTTL != 3*time.Hour || defs.containerReadOnlyExtraRepoTTL != 48*time.Hour || defs.containerReapKeep != 12 {
		t.Errorf("bundle container defaults = %+v", defs)
	}
	if defs.agentWorkflowDefault != workflowDirectToMain {
		t.Errorf("bundle workflow default = %q, want direct-main", defs.agentWorkflowDefault)
	}
	if defs.agentWorkflowRepos["coilyco-flight-deck/ward"] != workflowPullRequestAndMerge {
		t.Errorf("bundle ward workflow = %q, want pull-requests-and-merge", defs.agentWorkflowRepos["coilyco-flight-deck/ward"])
	}
}

func TestSmartDefaultsRejectsBadConfigRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	if _, err := currentSmartDefaultsWithError(); err == nil {
		t.Fatal("currentSmartDefaultsWithError with bad ref: want a loud config-source error")
	}
}

func TestSmartDefaultsRejectsMalformedValue(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-reservation-ttl "nope"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleDefaultsKDLPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write malformed defaults bundle: %v", err)
	}
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(dir)); err == nil {
		t.Fatal("malformed smart defaults selected a bundle; want a loud parse error")
	}
}

func TestSmartDefaultsBundleMissingFileFailsLoud(t *testing.T) {
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(t.TempDir())); err == nil {
		t.Fatal("bundle without smart defaults selected a source; want a loud read error")
	}
}

func TestSmartDefaultsRejectsInvalidWorkflow(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-workflow default="merge-it"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleDefaultsKDLPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write malformed defaults bundle: %v", err)
	}
	if _, err := loadSmartDefaultsFrom(bundleConfigSource(dir)); err == nil {
		t.Fatal("invalid workflow default selected a bundle; want a loud parse error")
	}
}

func TestSmartDefaultsRejectsMissingRepoAuthority(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-reservation-ttl "2h"
}`
	if err := os.WriteFile(filepath.Join(dir, bundleDefaultsKDLPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	if _, err := currentSmartDefaultsWithError(); err == nil {
		t.Fatal("bundle without repo-authority selected a source; want a loud parse error")
	}
}
