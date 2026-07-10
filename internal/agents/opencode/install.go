package opencode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// Install performs the required opencode bootstrap. It tries to install the
// binary and fails loudly if it is still missing afterward.
func (a Agent) Install(rc agentsapi.RunCtx) error {
	if commandExists(record.Binary) {
		rc.Log("opencode already present in image; no install step required")
		return nil
	}
	if rc.Exec == nil {
		return fmt.Errorf("opencode binary %q is missing and no executor is available to install it", record.Binary)
	}
	rc.Log("opencode missing from image; attempting bootstrap install")
	if err := rc.Exec.Exec(rc.Ctx, "bash", "-c", "curl -fsSL https://opencode.ai/install | bash >&2"); err != nil {
		return fmt.Errorf("opencode install bootstrap failed: %w", err)
	}
	src := filepath.Join(rc.AgentHome, ".opencode", "bin", "opencode")
	if isExecutable(src) {
		if err := rc.Exec.Exec(rc.Ctx, "install", "-m", "0755", src, "/usr/local/bin/opencode"); err != nil {
			return fmt.Errorf("opencode install copy failed: %w", err)
		}
	}
	if !commandExists(record.Binary) {
		return fmt.Errorf("opencode install did not make %q available on PATH", record.Binary)
	}
	rc.Log("installed opencode harness")
	return nil
}

func commandExists(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
