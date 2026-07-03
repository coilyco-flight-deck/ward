package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/allowlist"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
	"github.com/urfave/cli/v3"

	"github.com/coilyco-flight-deck/ward/internal/agents/ollamaprobe"
)

// doctorCommand is ward's single diagnostic verb, running the allowlist and
// security check groups inline. See docs/doctor.md.
func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Run ward's diagnostic checks against the resolved config and host.",
		Description: "Validates the .ward/ward.yaml (or .coily/coily.yaml) allowlist " +
			"against the repo Makefile, then probes host security posture against the " +
			"parsed security: block: PATH posture per protected binary, passwordless sudo " +
			"(when forbid_passwordless is set), and credential-env scan. Also probes any " +
			"operator-configured local-harness Ollama endpoint (WARD_OLLAMA_URL / " +
			"WARD_GOOSE_OLLAMA_HOST_B64) so a down endpoint surfaces before a goose/opencode " +
			"dispatch would hang (ward#499). Exits non-zero on any FAIL row, and (per ward#450) " +
			"when no security: block is declared at all.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "skip",
				Usage: "Skip a probe (path | sudo | credentials | ollama). Repeatable.",
			},
			&cli.BoolFlag{
				Name:  "strict-credentials",
				Usage: "Fail (not warn) when a credential_env var is set in this session.",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			opts := doctorOptions{
				skips:             skipSet(cmd.StringSlice("skip")),
				strictCredentials: cmd.Bool("strict-credentials"),
			}
			return runDoctor(os.Stdout, opts)
		},
	}
}

// doctorOptions tunes ward doctor. Zero value runs every check with default
// (warn-not-fail) credential semantics and, per ward#450, fails on no security:.
type doctorOptions struct {
	skips             map[string]bool
	strictCredentials bool
	// allowMissingSecurity downgrades the ward#450 no-security: failure to a NOTE
	// (ward setup sets it; standalone doctor leaves it false, so CI stays gated).
	allowMissingSecurity bool
}

// runDoctor resolves the config path by the normal precedence, then runs every
// check against it. See runDoctorAt for the pinned-path variant ward setup uses.
func runDoctor(out io.Writer, opts doctorOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	yamlPath, err := resolveConfigPath(explicitConfigPath(), os.Getenv("WARD_CONFIG"), cwd)
	if err != nil {
		return err
	}
	return runDoctorAt(out, yamlPath, opts)
}

// runDoctorAt runs every check against an explicit config path, joining failures.
// Pinning the path lets ward setup validate the file it wrote (docs/setup.md).
func runDoctorAt(out io.Writer, yamlPath string, opts doctorOptions) error {
	var failures []string

	if err := runAllowlistAt(out, yamlPath); err != nil {
		failures = append(failures, "allowlist: "+err.Error())
	}
	if err := runSecurityAt(out, yamlPath, opts); err != nil {
		failures = append(failures, "security: "+err.Error())
	}
	if err := runOllamaAt(out, opts); err != nil {
		failures = append(failures, "ollama: "+err.Error())
	}

	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// runAllowlistAt delegates to cli-guard's allowlist.Lint engine for an explicit
// config path, rendering an OK summary or the collected Problems.
func runAllowlistAt(out io.Writer, yamlPath string) error {
	makefilePath := filepath.Join(filepath.Dir(filepath.Dir(yamlPath)), "Makefile")
	problems, err := allowlist.Lint(yamlPath, makefilePath)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return errors.New(renderAllowlistFailure(problems))
	}
	_, _ = fmt.Fprintf(out, "ward doctor allowlist: OK (%s)\n", yamlPath)
	return nil
}

// allowlistContractHint spells out what a "matching Makefile target" requires,
// appended to every allowlist failure. See docs/doctor.md (Allowlist contract).
const allowlistContractHint = "hint: a Makefile target matches only when it carries a `## <description>` " +
	"help comment whose text equals the command's `description:`, and `run:` is exactly `make <name>`. " +
	"A bare `target:` recipe with no `## ...` comment is not registered - `make <target>` still runs, but " +
	"doctor reports it as unmatched. See docs/doctor.md (Allowlist contract)."

// renderAllowlistFailure formats the collected Problems, one per line, followed
// by the contract hint. Pure so tests can drive it without touching disk.
func renderAllowlistFailure(problems []allowlist.Problem) string {
	msgs := make([]string, 0, len(problems)+1)
	for _, p := range problems {
		msgs = append(msgs, fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Msg))
	}
	msgs = append(msgs, allowlistContractHint)
	return strings.Join(msgs, "\n")
}

// missingSecurityHint is the ward#450 remediation ward doctor reports as a FAIL
// (or ward setup as a NOTE) when a repo declares no security: block. See doctor.md.
const missingSecurityHint = "no `security:` block declared. As of ward#450 `ward doctor` fails by " +
	"default without one, so a passing run means a host-tool policy is in force, not merely that " +
	"nothing was misconfigured. The dev-verb gate (clean-tree, argv, and audit checks) still runs " +
	"without it, but no protected-binary / sudo / hook policy is enforced. Add a `security:` block to " +
	".ward/ward.yaml (`ward setup` scaffolds a commented template to uncomment and tailor). " +
	"Field reference: docs/ward-yaml.md (the security: schema)"

// runSecurityAt loads the config, runs the host probes to out, and errors on any
// FAIL row (or, per ward#450, a missing security: block unless allowed by opts).
func runSecurityAt(out io.Writer, yamlPath string, opts doctorOptions) error {
	cfg, err := repocfg.Load(yamlPath)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, summarizeSecurity(cfg.Security))
	if securityIsZero(cfg.Security) {
		if opts.allowMissingSecurity {
			_, _ = fmt.Fprintf(out, "  %-11s %-4s %s\n", "security", "NOTE", missingSecurityHint)
			return nil
		}
		return errors.New(missingSecurityHint)
	}

	var results []probeResult
	if !opts.skips["path"] {
		results = append(results, runPathPosture(cfg.Security.ProtectedBinaries, exec.LookPath)...)
	} else {
		results = append(results, probeResult{probe: "path", severity: sevSkip, detail: "skipped by --skip path"})
	}
	if !opts.skips["sudo"] {
		results = append(results, runSudoProbe(cfg.Security.Sudo.ForbidPasswordless, defaultSudoRunner))
	} else {
		results = append(results, probeResult{probe: "sudo", severity: sevSkip, detail: "skipped by --skip sudo"})
	}
	if !opts.skips["credentials"] {
		results = append(results, runCredEnvProbe(cfg.Security.ProtectedBinaries, os.Getenv, opts.strictCredentials)...)
	} else {
		results = append(results, probeResult{probe: "credentials", severity: sevSkip, detail: "skipped by --skip credentials"})
	}

	var fails []string
	for _, r := range results {
		_, _ = fmt.Fprintf(out, "  %-11s %-4s %s\n", r.probe, r.severity, r.detail)
		if r.severity == sevFail {
			fails = append(fails, fmt.Sprintf("%s: %s", r.probe, r.detail))
		}
	}
	if len(fails) > 0 {
		return errors.New(strings.Join(fails, "; "))
	}
	return nil
}

// runOllamaAt renders the host-side local-harness reachability probe, the pre-dispatch
// mirror of the launch gate (ward#499). --skip ollama stands it down. See docs/doctor.md.
func runOllamaAt(out io.Writer, opts doctorOptions) error {
	if opts.skips[ollamaprobe.ProbeName] {
		_, _ = fmt.Fprintf(out, "  %-11s %-4s %s\n", ollamaprobe.ProbeName, sevSkip, "skipped by --skip "+ollamaprobe.ProbeName)
		return nil
	}
	var fails []string
	for _, r := range ollamaprobe.DoctorProbe(context.Background(), os.Getenv) {
		_, _ = fmt.Fprintf(out, "  %-11s %-4s %s\n", ollamaprobe.ProbeName, r.Severity, r.Detail)
		if r.Severity == ollamaprobe.SevFail {
			fails = append(fails, r.Detail)
		}
	}
	if len(fails) > 0 {
		return errors.New(strings.Join(fails, "; "))
	}
	return nil
}

// skipSet normalizes the --skip flag values into a lookup map. Unknown names
// are kept and silently ignored, so a typo doesn't break a passing CI step.
func skipSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[strings.ToLower(strings.TrimSpace(n))] = true
	}
	return out
}

// defaultSudoRunner runs `sudo -n true` and returns its stderr, exit code, and
// any spawn error. Exit 0 means it ran without a password (the failure mode).
func defaultSudoRunner() (string, int, error) {
	cmd := exec.Command("sudo", "-n", "true") // #nosec G204 -- fixed argv, no user input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stderr.String(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stderr.String(), exitErr.ExitCode(), nil
	}
	return stderr.String(), -1, err
}

// summarizeSecurity renders a one-line summary of the security: block. Kept
// pure so tests can drive it from synthesized values without touching disk.
func summarizeSecurity(sec repocfg.Security) string {
	if securityIsZero(sec) {
		return "ward doctor security: no security: declared"
	}
	sudo := "unrestricted"
	if sec.Sudo.ForbidPasswordless {
		sudo = "forbid_passwordless"
	}
	hooks := "none"
	if len(sec.Hooks.DenyBareBinaries) > 0 || len(sec.Hooks.RouteHints) > 0 {
		hooks = fmt.Sprintf("%d deny / %d route-hint", len(sec.Hooks.DenyBareBinaries), len(sec.Hooks.RouteHints))
	}
	return fmt.Sprintf("ward doctor security: %d protected, sudo=%s, hooks=%s",
		len(sec.ProtectedBinaries), sudo, hooks)
}

// securityIsZero reports whether every field of sec is at its zero value.
// repocfg.Load returns a zero Security when the YAML has no security: block.
func securityIsZero(sec repocfg.Security) bool {
	return len(sec.ProtectedBinaries) == 0 &&
		!sec.Sudo.ForbidPasswordless &&
		len(sec.Hooks.DenyBareBinaries) == 0 &&
		len(sec.Hooks.RouteHints) == 0
}
