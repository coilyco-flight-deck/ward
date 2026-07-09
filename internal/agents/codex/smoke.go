package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agents/modelconfig"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

const codexProbeGate = "codex-probe"

// PreLaunchCheck runs a bounded codex exec with the resolved model so a stale
// model string fails before launch instead of silently falling through.
func (a Agent) PreLaunchCheck(rc agentsapi.RunCtx) error {
	if !rc.Headless {
		return nil
	}
	if os.Getenv("WARD_SMOKE_TEST_SKIP") == "1" {
		rc.Log("codex launch probe skipped (WARD_SMOKE_TEST_SKIP=1)")
		return nil
	}
	rc.Log("codex launch probe: probing codex model %q before launch", rc.CodexModel)
	probeCtx := rc.Ctx
	if probeCtx == nil {
		probeCtx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(probeCtx, 30*time.Second)
	defer cancel()
	out, stderr, code := captureProbeInput(probeCtx, codexProbeArgv(rc), codexProbePrompt)
	if serr := classifyCodexProbeFailure(rc.CodexModel, out, stderr, code); serr != nil {
		return serr
	}
	rc.Log("codex launch probe: model probe passed, proceeding")
	return nil
}

const codexProbePrompt = "Reply with exactly ok."

func codexProbeArgv(rc agentsapi.RunCtx) []string {
	argv := setprivPrefix(rc)
	argv = append(argv, "codex", "exec",
		"--skip-git-repo-check",
		"--ephemeral",
		"--ignore-user-config",
		"--sandbox", "danger-full-access",
	)
	if rc.CodexModel != "" {
		argv = append(argv, "--model", rc.CodexModel)
	}
	return append(argv, "-")
}

// setprivPrefix builds the launch prefix that pins HOME/CODEX_HOME and, when the
// container resolved an agent uid/gid, drops to that identity like the launch path.
func setprivPrefix(rc agentsapi.RunCtx) []string {
	argv := make([]string, 0, 7)
	if rc.AgentUID != "" && rc.AgentGID != "" {
		argv = append(argv, "setpriv", "--reuid="+rc.AgentUID, "--regid="+rc.AgentGID, "--init-groups")
	}
	argv = append(argv, "env", "HOME="+rc.AgentHome, "CODEX_HOME="+rc.AgentHome+"/.codex")
	return argv
}

func classifyCodexProbeFailure(model, out, stderr string, code int) error {
	combined := strings.TrimSpace(out + "\n" + stderr)
	if combined == "" {
		if code == 0 {
			return nil
		}
		return agentsapi.NewGateError(codexProbeGate, fmt.Errorf("codex launch probe exited %d without output", code))
	}
	if model != "" && modelconfig.LooksLike(combined) {
		return agentsapi.NewGateError(modelconfig.GateName, fmt.Errorf("codex model %q was rejected by the launch probe (ward#670): %s", model, oneLine(combined)))
	}
	if strings.Contains(combined, "Reading additional input from stdin") {
		return agentsapi.NewGateError(codexProbeGate, fmt.Errorf("codex launch probe stalled waiting for stdin (exit %d): %s. Ward now feeds the prompt on stdin with `codex exec -`, so this usually means a stale Codex CLI or a wrapper that still blocks on stdin", code, oneLine(combined)))
	}
	if code == 0 {
		return nil
	}
	return agentsapi.NewGateError(codexProbeGate, fmt.Errorf("codex launch probe failed (exit %d): %s", code, oneLine(combined)))
}

func captureProbe(ctx context.Context, argv []string) (stdout, stderr string, code int) {
	return captureProbeInput(ctx, argv, "")
}

func captureProbeInput(ctx context.Context, argv []string, stdin string) (stdout, stderr string, code int) {
	devnull, _ := os.Open(os.DevNull)
	if devnull != nil {
		defer func() { _ = devnull.Close() }()
	}
	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- fixed argv
	if stdin == "" {
		cmd.Stdin = devnull
	} else {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		code = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return outBuf.String(), errBuf.String(), code
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
