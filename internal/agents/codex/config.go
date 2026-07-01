package codex

import (
	"os"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// ComposeConfig writes codex's approvals-off, sandbox-open config in-container
// (ward#178), cheapest-settings-by-default from the RunCtx knobs (ward#379).
func (a Agent) ComposeConfig(rc agentsapi.RunCtx) error {
	dir := filepath.Join(rc.AgentHome, ".codex")
	_ = os.MkdirAll(dir, 0o755)
	body := "# Written by the ward container entrypoint (ward#178): container is the boundary.\n" +
		"approval_policy = \"never\"\n" +
		"sandbox_mode = \"danger-full-access\"\n" +
		"# Cheapest codex settings by default (ward#379); override with WARD_CODEX_*.\n" +
		"model = \"" + rc.CodexModel + "\"\n" +
		"model_reasoning_effort = \"" + rc.CodexEffort + "\"\n" +
		"model_verbosity = \"" + rc.CodexVerbosity + "\"\n"
	out := filepath.Join(dir, "config.toml")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		rc.Log("could not write codex config: %v", werr)
		return nil
	}
	rc.Log("wrote codex config (approvals off, sandbox open, model %s / effort %s / verbosity %s) to %s", rc.CodexModel, rc.CodexEffort, rc.CodexVerbosity, out)
	return nil
}
