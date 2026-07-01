package claude

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// SeedOnboarding writes ~/.claude.json so interactive claude skips its first-run
// gates: theme picker (ward#305) + bypass-mode/folder-trust (ward#313).
func (a Agent) SeedOnboarding(rc agentsapi.RunCtx) error {
	work := "/workspace/" + rc.TargetName
	cfg := map[string]any{
		"hasCompletedOnboarding":        true,
		"theme":                         "dark",
		"bypassPermissionsModeAccepted": true,
		"projects": map[string]any{
			work: map[string]any{
				"hasTrustDialogAccepted":        true,
				"hasCompletedProjectOnboarding": true,
			},
		},
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
