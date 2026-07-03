package claude

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// SeedOnboarding writes ~/.claude.json so claude skips its first-run gates (theme
// ward#305, bypass/trust ward#313), trusting every rc.TrustDirs entry (ward#168).
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
		rc.Log("could not build claude onboarding config: %v", merr)
		return nil
	}
	out := filepath.Join(rc.AgentHome, ".claude.json")
	if werr := os.WriteFile(out, data, 0o644); werr != nil { // #nosec G306 -- onboarding flags, not a secret
		rc.Log("could not seed claude onboarding: %v", werr)
		return nil
	}
	rc.Log("seeded claude onboarding (skip first-run wizard + bypass/trust gates) at %s", out)
	return nil
}
