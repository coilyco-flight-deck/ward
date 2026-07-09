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

func TestSmartDefaultsBundleRef(t *testing.T) {
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
    agent-workflow default="direct-to-main" {
        repo "coilyco-flight-deck/ward" workflow="pull-request"
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
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("currentSmartDefaultsWithError(bundle): %v", err)
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
		t.Errorf("bundle workflow default = %q, want direct-to-main", defs.agentWorkflowDefault)
	}
	if defs.agentWorkflowRepos["coilyco-flight-deck/ward"] != workflowPullRequest {
		t.Errorf("bundle ward workflow = %q, want pull-request", defs.agentWorkflowRepos["coilyco-flight-deck/ward"])
	}
	if got := defs.trustedOwnerList(); len(got) != 4 || got[0] != "coilysiren" || got[1] != "coilyco-bridge" {
		t.Errorf("bundle trusted owners = %v, want the configured owner set", got)
	}
	if defs.repoAuthorityDefault != forgeForgejo {
		t.Errorf("bundle repo default forge = %v, want forgejo", defs.repoAuthorityDefault)
	}
	if defs.forgeForRepo("coilysiren", "agentic-os") != forgeGitHub {
		t.Errorf("bundle coilysiren repo forge = %v, want github", defs.forgeForRepo("coilysiren", "agentic-os"))
	}
	if defs.forgeForRepo("coilyco-flight-deck", "ward") != forgeForgejo {
		t.Errorf("bundle coilyco repo forge = %v, want forgejo", defs.forgeForRepo("coilyco-flight-deck", "ward"))
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
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	if _, err := currentSmartDefaultsWithError(); err == nil {
		t.Fatal("malformed smart defaults selected a bundle; want a loud parse error")
	}
}

func TestSmartDefaultsBundleMissingFileFailsLoud(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+t.TempDir())
	if _, err := currentSmartDefaultsWithError(); err == nil {
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
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	if _, err := currentSmartDefaultsWithError(); err == nil {
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
