package claude

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

// smokeTestDiskPaths are the mounts whose exhaustion stalls claude at startup:
// / (image + ~/.claude) and /workspace (the clone). Probed before the wait (ward#273).
var smokeTestDiskPaths = []string{"/", "/workspace"}

// smokeTestDiskFloorBytes is the free-space floor below which a claude startup
// hang is more likely disk exhaustion than an auth problem (ward#273).
const smokeTestDiskFloorBytes = 512 * 1024 * 1024 // 512MiB

// authErrorMarkers mark a real credential rejection, not a disk/network hang, so
// re-login is suggested only on a true auth failure (synced with entrypoint.sh).
var authErrorMarkers = []string{
	"not logged in",
	"401",
	"invalid api key",
	"authentication_error",
	"unauthorized",
	"please run /login",
}

// PreLaunchCheck ports smoke_test_claude_auth: a bounded claude probe whose
// timeout/non-auth stall reports disk, not the login (ward#222, ward#273).
func (a Agent) PreLaunchCheck(rc agentsapi.RunCtx) error {
	if !rc.Headless {
		return nil
	}
	if os.Getenv("WARD_SMOKE_TEST_SKIP") == "1" {
		rc.Log("auth smoke test skipped (WARD_SMOKE_TEST_SKIP=1)")
		return nil
	}
	if !commandExists("claude") {
		return nil
	}
	// Pre-flight headroom (ward#273): surface a near-full disk now, before the
	// 90s wait, so a disk problem cannot masquerade as an auth problem later.
	if low := lowDiskPaths(smokeTestDiskPaths, smokeTestDiskFloorBytes); len(low) > 0 {
		rc.Log("auth smoke test: WARNING low disk before probe - %s; a claude startup hang here is likely disk exhaustion, not credentials (ward#273)", diskReport(smokeTestDiskPaths))
	}
	rc.Log("auth smoke test: probing claude before launch (ward#222)")
	probeCtx := rc.Ctx
	if probeCtx == nil {
		probeCtx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(probeCtx, 90*time.Second)
	defer cancel()
	argv := claudeProbeArgv(rc)
	out, stderr, code := captureProbe(probeCtx, argv)
	if serr := classifyClaudeModelConfigFailure(rc.ClaudeModel, out, stderr, code); serr != nil {
		return serr
	}
	if probeCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("auth smoke test: claude -p did not respond within 90s - a startup hang, not necessarily an auth problem (ward#222, ward#273). Likely causes: a full disk (claude cannot write ~/.claude), network, or a slow cold start. Disk: %s. If disk is low, free space on the Docker host; otherwise refresh the host claude login (re-run 'claude' on the host) and relaunch. WARD_SMOKE_TEST_SKIP=1 bypasses", diskReport(smokeTestDiskPaths))
	}
	if code != 0 || strings.TrimSpace(out) == "" {
		if looksLikeAuthError(stderr) || looksLikeAuthError(out) {
			return fmt.Errorf("auth smoke test: claude -p rejected the credentials (exit %d) - they are unusable in-container (ward#222). Refresh the host claude login (re-run 'claude' on the host) and relaunch; WARD_SMOKE_TEST_SKIP=1 bypasses", code)
		}
		return fmt.Errorf("auth smoke test: claude -p produced no usable output (exit %d) without an auth error - more likely a disk/network/startup problem than credentials (ward#222, ward#273). Disk: %s. WARD_SMOKE_TEST_SKIP=1 bypasses", code, diskReport(smokeTestDiskPaths))
	}
	rc.Log("auth smoke test: claude responded, auth OK")
	return nil
}

// claudeProbeArgv builds the bounded auth/model probe argv, including a resolved
// --model when the fleet config carries one.
func claudeProbeArgv(rc agentsapi.RunCtx) []string {
	argv := append(setprivPrefix(rc), "claude", "-p", "--output-format", "json")
	if rc.ClaudeModel != "" {
		argv = append(argv, "--model", rc.ClaudeModel)
	}
	return append(argv, "Reply with the single word: ok")
}

// classifyClaudeModelConfigFailure turns a stale model rejection into the named
// model-config gate. Non-model failures still follow the auth/disk split.
func classifyClaudeModelConfigFailure(model, out, stderr string, code int) error {
	model = strings.TrimSpace(model)
	if model == "" || code == 0 {
		return nil
	}
	combined := strings.TrimSpace(out + "\n" + stderr)
	if combined == "" || !modelconfig.LooksLike(combined) {
		return nil
	}
	return agentsapi.NewGateError(modelconfig.GateName, fmt.Errorf("claude model %q was rejected by the launch probe (ward#670): %s", model, oneLine(combined)))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// setprivPrefix builds the launch prefix that drops to the agent uid/gid with
// init-groups and pins HOME (`setpriv ... env HOME=<home>`), matching core's.
func setprivPrefix(rc agentsapi.RunCtx) []string {
	return []string{
		"setpriv", "--reuid=" + rc.AgentUID, "--regid=" + rc.AgentGID, "--init-groups",
		"env", "HOME=" + rc.AgentHome,
	}
}

// looksLikeAuthError reports whether s carries a genuine credential-rejection
// marker, so re-login is only suggested for real auth failures (ward#273).
func looksLikeAuthError(s string) bool {
	l := strings.ToLower(s)
	for _, m := range authErrorMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// commandExists reports whether bin is on PATH (the bash `command -v`).
func commandExists(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// lowDiskPaths returns the subset of paths whose free space is below floor
// bytes. Unstattable paths are skipped (ward#273).
func lowDiskPaths(paths []string, floor uint64) []string {
	var low []string
	for _, p := range paths {
		free, _, err := diskFreeBytes(p)
		if err != nil {
			continue
		}
		if free < floor {
			low = append(low, p)
		}
	}
	return low
}

// diskReport renders free/total disk per path as one string, e.g.
// "/ 1.2GiB free of 50.0GiB; ...". Unstattable paths are skipped (ward#273).
func diskReport(paths []string) string {
	var parts []string
	for _, p := range paths {
		free, total, err := diskFreeBytes(p)
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s free of %s", p, diskBytes(free), diskBytes(total)))
	}
	if len(parts) == 0 {
		return "disk usage unavailable"
	}
	return strings.Join(parts, "; ")
}

// diskBytes renders a byte count compactly in binary units, spanning B..EiB so
// multi-GiB disk totals read clean (ward#273).
func diskBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// captureProbe runs the smoke-test argv with /dev/null stdin and a capped stderr
// capture, returning stdout, stderr, rc; stderr feeds the auth-vs-disk split (ward#273).
func captureProbe(ctx context.Context, argv []string) (stdout, stderr string, code int) {
	devnull, _ := os.Open(os.DevNull)
	if devnull != nil {
		defer func() { _ = devnull.Close() }()
	}
	errBuf := &capBuffer{max: 8192}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- fixed setpriv/claude argv
	cmd.Stdin = devnull
	cmd.Stderr = errBuf
	out, err := cmd.Output()
	code = 0
	if err != nil {
		code = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
	}
	return string(out), errBuf.String(), code
}

// capBuffer retains at most max bytes but always reports a full write, so a probe
// streaming endless stderr neither blocks nor balloons memory (ward#273).
type capBuffer struct {
	b   bytes.Buffer
	max int
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		if room < len(p) {
			_, _ = c.b.Write(p[:room])
		} else {
			_, _ = c.b.Write(p)
		}
	}
	return len(p), nil // claim full write so the child never blocks on a full pipe
}

func (c *capBuffer) String() string { return c.b.String() }
