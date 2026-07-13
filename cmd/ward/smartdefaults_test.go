package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSmartDefaultsBaked(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "")
	t.Setenv("WARD_TARGET_OWNER", "coilysiren")
	t.Setenv("WARD_TARGET_REPO", "coilysiren/example")
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("currentSmartDefaultsWithError(baked): %v", err)
	}
	if defs.agentReservationTTL != 3*time.Hour {
		t.Errorf("baked reservation ttl = %s, want 3h", defs.agentReservationTTL)
	}
	if defs.agentImage != containerImageDefault || defs.agentTag != containerImageTagDefault {
		t.Errorf("baked agent image/tag = %q:%q, want %q:%q", defs.agentImage, defs.agentTag, containerImageDefault, containerImageTagDefault)
	}
	if defs.engineerContainerLimit != 12 || defs.directorMaxParallel != 10 || defs.directorLimit != 50 || defs.containerReapKeep != 10 {
		t.Errorf("baked defaults = %+v, want the neutral policy bundle", defs)
	}
	if len(defs.trustedOwners) == 0 || defs.trustedOwners[0] != "coilysiren" {
		t.Errorf("baked trusted owners = %v, want coilysiren", defs.trustedOwners)
	}
	if len(defs.repoAuthorityRules) == 0 || defs.repoAuthorityRules[0].Pattern != "coilysiren/*" || defs.repoAuthorityRules[0].Forge != forgeGitHub {
		t.Errorf("baked repo authority = %+v, want coilysiren/* on github", defs.repoAuthorityRules)
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
    engineer-container-limit "17"
    engineer-open-pr-branch-limit "8"
    director-max-parallel "13"
    director-limit "77"
    director-poll-interval "45s"
    reviewer-timeout "11m"
    config-bundle-ttl "900"
    container-assets-ttl "3h"
    container-read-only-extra-repo-ttl "48h"
    container-reap-keep "12"
    agent-workflow default="merge-remote-main" {
    }
}
`
	reposBody := `repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/*" forge=github
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
	if defs.engineerContainerLimit != 17 || defs.directorMaxParallel != 13 || defs.directorLimit != 77 || defs.directorPollInterval != 45*time.Second {
		t.Errorf("bundle director defaults = %+v", defs)
	}
	if defs.engineerOpenPRBranchLimit != 8 {
		t.Errorf("bundle open-pr defaults = %+v", defs)
	}
	if defs.reviewerTimeout != 11*time.Minute || defs.configBundleTTL != 15*time.Minute {
		t.Errorf("bundle duration defaults = %+v", defs)
	}
	if defs.containerAssetsTTL != 3*time.Hour || defs.containerReadOnlyExtraRepoTTL != 48*time.Hour || defs.containerReapKeep != 12 {
		t.Errorf("bundle container defaults = %+v", defs)
	}
	if defs.agentWorkflowDefault != workflowDirectToMain {
		t.Errorf("bundle workflow default = %q, want merge-remote-main", defs.agentWorkflowDefault)
	}
	if len(defs.agentWorkflowRepos) != 0 {
		t.Errorf("bundle workflow overrides = %v, want none in the neutral starter", defs.agentWorkflowRepos)
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
    agent-reservation-ttl "3h"
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

func TestSmartDefaultsRejectsMissingRepoAuthority(t *testing.T) {
	dir := t.TempDir()
	body := `defaults {
    agent-reservation-ttl "3h"
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

// TestSmartDefaultsFailureNamesTheConfigSource pins the attribution contract:
// a fail-closed defaults error names the bundle that served the value (the
// aos#452 stale-pin incident was undiagnosable from the value alone).
func TestSmartDefaultsFailureNamesTheConfigSource(t *testing.T) {
	dir := t.TempDir()
	// 1h undercuts the built-in engineer 90m limit: trips the TTL invariant.
	if err := os.WriteFile(filepath.Join(dir, "defaults.kdl"),
		[]byte("defaults {\n    agent-reservation-ttl \"1h\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repos.kdl"),
		[]byte("repos {\n    repo-authority default=forgejo {\n        trusted-owner coilysiren\n    }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "file://" + filepath.ToSlash(dir)
	t.Setenv(wardConfigRefEnv, ref)

	_, err := currentSmartDefaultsWithError()
	if err == nil || !strings.Contains(err.Error(), "must exceed role") {
		t.Fatalf("want the reservation-TTL invariant failure, got %v", err)
	}
	if !strings.Contains(err.Error(), ref) {
		t.Errorf("error does not name the serving config source %q:\n%v", ref, err)
	}
}
