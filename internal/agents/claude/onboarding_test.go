package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// noopLog is the discard Logger the folder tests pass in rc.Log.
func noopLog(string, ...any) {}

// seededOnboarding is the parsed ~/.claude.json shape the trust tests assert on.
type seededOnboarding struct {
	HasCompletedOnboarding        bool `json:"hasCompletedOnboarding"`
	BypassPermissionsModeAccepted bool `json:"bypassPermissionsModeAccepted"`
	Projects                      map[string]struct {
		HasTrustDialogAccepted        bool `json:"hasTrustDialogAccepted"`
		HasCompletedProjectOnboarding bool `json:"hasCompletedProjectOnboarding"`
	} `json:"projects"`
}

// readSeed runs SeedOnboarding for rc and returns the parsed ~/.claude.json.
func readSeed(t *testing.T, rc agentsapi.RunCtx) seededOnboarding {
	t.Helper()
	if err := (Agent{}).SeedOnboarding(rc); err != nil {
		t.Fatalf("SeedOnboarding: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(rc.AgentHome, ".claude.json"))
	if err != nil {
		t.Fatalf("expected ~/.claude.json: %v", err)
	}
	var got seededOnboarding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("claude.json is not valid JSON: %v\n%s", err, data)
	}
	if !got.HasCompletedOnboarding {
		t.Errorf("claude.json missing onboarding flag: %s", data)
	}
	if !got.BypassPermissionsModeAccepted {
		t.Errorf("claude.json missing bypassPermissionsModeAccepted: %s", data)
	}
	return got
}

// assertTrusted fails unless the dir carries both folder-trust flags.
func assertTrusted(t *testing.T, got seededOnboarding, dir string) {
	t.Helper()
	proj, ok := got.Projects[dir]
	if !ok {
		t.Fatalf("claude.json missing projects[%s]: %+v", dir, got.Projects)
	}
	if !proj.HasTrustDialogAccepted || !proj.HasCompletedProjectOnboarding {
		t.Errorf("claude.json missing folder-trust flags for %s: %+v", dir, proj)
	}
}

// TestSeedOnboarding covers ward#305/ward#313: with no TrustDirs, the seed writes
// the onboarding/bypass flags and falls back to trusting the target clone.
func TestSeedOnboarding(t *testing.T) {
	home := t.TempDir()
	got := readSeed(t, agentsapi.RunCtx{AgentHome: home, TargetName: "ward", Log: noopLog})
	assertTrusted(t, got, "/workspace/ward")
}

// TestSeedOnboardingTrustDirs covers ward#168: every TrustDirs entry (target,
// /workspace, extra repos, /substrate repos) is pre-trusted, none dropped.
func TestSeedOnboardingTrustDirs(t *testing.T) {
	home := t.TempDir()
	dirs := []string{
		"/workspace/ward",
		"/workspace",
		"/workspace/cli-guard",
		"/substrate",
		"/substrate/reference-project",
	}
	got := readSeed(t, agentsapi.RunCtx{AgentHome: home, TargetName: "ward", TrustDirs: dirs, Log: noopLog})
	for _, dir := range dirs {
		assertTrusted(t, got, dir)
	}
	if len(got.Projects) != len(dirs) {
		t.Errorf("projects count = %d, want %d: %+v", len(got.Projects), len(dirs), got.Projects)
	}
}
