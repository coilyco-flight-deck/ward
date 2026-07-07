package codex

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// SeedOnboarding writes ~/.codex.json so codex skips its first-run gates and
// trusts every rc.TrustDirs entry, matching claude's startup trust set.
func (a Agent) SeedOnboarding(rc agentsapi.RunCtx) error {
	dirs := rc.TrustDirs
	if len(dirs) == 0 {
		dirs = []string{"/workspace/" + rc.TargetName}
	}
	projects := make(map[string]any, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		projects[dir] = map[string]any{
			"hasTrustDialogAccepted":        true,
			"hasCompletedProjectOnboarding": true,
		}
	}
	cfg := map[string]any{
		"hasCompletedOnboarding":        true,
		"theme":                         "dark",
		"bypassPermissionsModeAccepted": true,
		"projects":                      projects,
	}
	data, merr := json.Marshal(cfg)
	if merr != nil {
		rc.Log("could not build codex onboarding config: %v", merr)
		return nil
	}
	out := filepath.Join(rc.AgentHome, ".codex.json")
	if werr := os.WriteFile(out, data, 0o644); werr != nil { // #nosec G306 -- onboarding flags, not a secret
		rc.Log("could not seed codex onboarding: %v", werr)
		return nil
	}
	rc.Log("seeded codex onboarding (skip first-run wizard + bypass/trust gates) at %s", out)
	return nil
}
