package opencode

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// Install self-installs the opencode binary (absent from the dev-base image) at
// bootstrap; a no-op when it is already present. Best-effort like the bash.
func (a Agent) Install(rc agentsapi.RunCtx) error {
	if commandExists("opencode") {
		rc.Log("opencode already present in image; skipping install")
		return nil
	}
	rc.Log("installing opencode (local Ollama-backed harness; not baked into the dev-base image yet)")
	// The bash pipes `curl ... | bash`; reproduce via `bash -c` so the installer's
	// own redirects to stderr are preserved (its stdout is the script, not output).
	_ = rc.Exec.Exec(rc.Ctx, "bash", "-c", "curl -fsSL https://opencode.ai/install | bash >&2")
	src := filepath.Join(os.Getenv("HOME"), ".opencode", "bin", "opencode")
	if isExecutable(src) {
		_ = rc.Exec.Exec(rc.Ctx, "install", "-m", "0755", src, "/usr/local/bin/opencode")
	}
	if !commandExists("opencode") {
		rc.Log("opencode install failed; opencode mode will abort before launch (use --image with opencode baked in, or fix network)")
	}
	return nil
}

// commandExists reports whether bin is on PATH (the bash `command -v`).
func commandExists(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// isExecutable reports whether path exists and has any execute bit (`[ -x ]`).
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
