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

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/allowlist"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
	"github.com/urfave/cli/v3"
)

// doctorCommand is ward's single diagnostic verb. See docs/doctor.md.
//
// Runs every check group inline: allowlist (yaml ↔ Makefile contract via
// cli-guard's allowlist package) and security (parse summary + host probes).
// Reads the resolved config path (--config > $WARD_CONFIG > walk-up, per #38).
func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Run ward's diagnostic checks against the resolved config and host.",
		Description: "Validates the .ward/ward.yaml (or .coily/coily.yaml) allowlist " +
			"against the repo Makefile, then probes host security posture against the " +
			"parsed security: block: PATH posture per protected binary, passwordless sudo " +
			"(when forbid_passwordless is set), and credential-env scan. Exits non-zero " +
			"on any FAIL row.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "skip",
				Usage: "Skip a security probe (path | sudo | credentials). Repeatable.",
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

// doctorOptions tunes ward doctor. Zero value means "run every check with
// default (warn-not-fail) credential semantics".
type doctorOptions struct {
	skips             map[string]bool
	strictCredentials bool
}

// runDoctor runs every check inline, writes per-group output to out, and
// returns a joined error when any check failed. Partial output flushes
// either way so an operator sees what passed before the failure summary.
func runDoctor(out io.Writer, opts doctorOptions) error {
	var failures []string

	if err := runAllowlist(out); err != nil {
		failures = append(failures, "allowlist: "+err.Error())
	}
	if err := runSecurity(out, opts); err != nil {
		failures = append(failures, "security: "+err.Error())
	}

	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// runAllowlist resolves the config path and delegates to cli-guard's
// allowlist.Lint engine. Renders a one-line OK summary or the collected
// Problems joined by newline.
func runAllowlist(out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	yamlPath, err := resolveConfigPath(explicitConfigPath(), os.Getenv("WARD_CONFIG"), cwd)
	if err != nil {
		return err
	}
	makefilePath := filepath.Join(filepath.Dir(filepath.Dir(yamlPath)), "Makefile")
	problems, err := allowlist.Lint(yamlPath, makefilePath)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		msgs := make([]string, 0, len(problems))
		for _, p := range problems {
			msgs = append(msgs, fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Msg))
		}
		return errors.New(strings.Join(msgs, "\n"))
	}
	_, _ = fmt.Fprintf(out, "ward doctor allowlist: OK (%s)\n", yamlPath)
	return nil
}

// runSecurity loads the resolved config, runs the host probes, writes
// per-row results to out, and returns a non-nil error when any FAIL
// row surfaced.
func runSecurity(out io.Writer, opts doctorOptions) error {
	cfg, err := loadDefault()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, summarizeSecurity(cfg.Security))
	if securityIsZero(cfg.Security) {
		return nil
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

// skipSet normalizes the --skip flag values into a lookup map. Unknown
// names are kept; doctor silently ignores them rather than erroring, so a
// typo doesn't break a CI step that already passed before the typo's group
// existed.
func skipSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[strings.ToLower(strings.TrimSpace(n))] = true
	}
	return out
}

// defaultSudoRunner runs `sudo -n true` and returns the captured stderr,
// exit code, and any spawn error. The probe interprets exit 0 as "ran
// without password" (the failure mode under forbid_passwordless).
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
