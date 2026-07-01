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

// TestSeedOnboarding covers ward#305/ward#313: SeedOnboarding writes ~/.claude.json
// with the nested onboarding + bypass + folder-trust flags for the launch cwd.
func TestSeedOnboarding(t *testing.T) {
	home := t.TempDir()
	rc := agentsapi.RunCtx{AgentHome: home, TargetName: "ward", Log: noopLog}
	if err := (Agent{}).SeedOnboarding(rc); err != nil {
		t.Fatalf("SeedOnboarding: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("expected ~/.claude.json: %v", err)
	}
	var got struct {
		HasCompletedOnboarding        bool `json:"hasCompletedOnboarding"`
		BypassPermissionsModeAccepted bool `json:"bypassPermissionsModeAccepted"`
		Projects                      map[string]struct {
			HasTrustDialogAccepted        bool `json:"hasTrustDialogAccepted"`
			HasCompletedProjectOnboarding bool `json:"hasCompletedProjectOnboarding"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("claude.json is not valid JSON: %v\n%s", err, data)
	}
	if !got.HasCompletedOnboarding {
		t.Errorf("claude.json missing onboarding flag: %s", data)
	}
	if !got.BypassPermissionsModeAccepted {
		t.Errorf("claude.json missing bypassPermissionsModeAccepted: %s", data)
	}
	proj, ok := got.Projects["/workspace/ward"]
	if !ok {
		t.Fatalf("claude.json missing projects[/workspace/ward]: %s", data)
	}
	if !proj.HasTrustDialogAccepted || !proj.HasCompletedProjectOnboarding {
		t.Errorf("claude.json missing folder-trust flags for launch cwd: %s", data)
	}
}
