package codex

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// ComposeConfig writes Codex's approvals-off, sandbox-open config with cheapest
// defaults, a headless rate-limit nudge opt-out, and [projects] folder trust.
func (a Agent) ComposeConfig(rc agentsapi.RunCtx) error {
	dir := filepath.Join(rc.AgentHome, ".codex")
	_ = os.MkdirAll(dir, 0o755)
	body := "# Written by the ward container entrypoint (ward#178): container is the boundary.\n" +
		"approval_policy = \"never\"\n" +
		"sandbox_mode = \"danger-full-access\"\n" +
		"# Headless containers cannot answer Codex's optional model-switch reminder.\n" +
		"notice.hide_rate_limit_model_nudge = true\n" +
		"# Cheapest codex settings by default (ward#379); override with WARD_CODEX_*.\n" +
		"model = \"" + rc.CodexModel + "\"\n" +
		"model_reasoning_effort = \"" + rc.CodexEffort + "\"\n" +
		"model_verbosity = \"" + rc.CodexVerbosity + "\"\n" +
		trustProjectsTOML(rc)
	out := filepath.Join(dir, "config.toml")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		rc.Log("could not write codex config: %v", werr)
		return nil
	}
	rc.Log("wrote codex config (approvals off, sandbox open, model %s / effort %s / verbosity %s, %d trusted dirs) to %s",
		rc.CodexModel, rc.CodexEffort, rc.CodexVerbosity, len(trustDirs(rc)), out)
	return nil
}

// trustDirs is the workspace trust set for the [projects] tables: every
// rc.TrustDirs entry, falling back to the target clone when the set is empty.
func trustDirs(rc agentsapi.RunCtx) []string {
	dirs := make([]string, 0, len(rc.TrustDirs))
	for _, dir := range rc.TrustDirs {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		dirs = []string{"/workspace/" + rc.TargetName}
	}
	return dirs
}

// trustProjectsTOML renders the folder-trust tables codex actually reads (ward#678):
// `[projects."<dir>"]` + `trust_level = "trusted"`, matching claude's startup trust set.
func trustProjectsTOML(rc agentsapi.RunCtx) string {
	var b strings.Builder
	b.WriteString("# Workspace folder trust, matching claude's startup trust set (ward#168, ward#678).\n")
	for _, dir := range trustDirs(rc) {
		b.WriteString("[projects." + tomlBasicString(dir) + "]\n")
		b.WriteString("trust_level = \"trusted\"\n")
	}
	return b.String()
}

// tomlBasicString quotes s as a TOML basic string (the [projects] dotted key).
func tomlBasicString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
