package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
	"github.com/urfave/cli/v3"
)

// doctorCommand is ward's diagnostic umbrella. See docs/doctor.md.
//
// Subcommands run a single check group; calling `ward doctor` with no
// subcommand runs every group and aggregates failures. Every group reads
// the same resolved config (--config > $WARD_CONFIG > walk-up, per #38).
func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Run ward's diagnostic check groups.",
		Description: "Without a subcommand, runs every check group and exits non-zero on any failure. " +
			"With a subcommand, runs that group only. Reads the resolved config path: " +
			"--config > $WARD_CONFIG > walk-up from cwd.",
		Action: func(_ context.Context, _ *cli.Command) error {
			return runAllDoctorGroups(os.Stdout)
		},
		Commands: []*cli.Command{
			{
				Name:  "allowlist",
				Usage: "Validate .ward/ward.yaml (or .coily/coily.yaml) against the repo Makefile.",
				Action: func(_ context.Context, _ *cli.Command) error {
					summary, err := runAllowlistCheck()
					if err != nil {
						return err
					}
					fmt.Println(summary)
					return nil
				},
			},
			{
				Name:  "security",
				Usage: "Probe host security posture against the parsed security: block.",
				Description: "Runs three probes against the resolved config: PATH posture per " +
					"protected binary, passwordless sudo (when forbid_passwordless is set), and " +
					"credential-env scan. Exits non-zero on any FAIL row. --skip suppresses a probe " +
					"(repeatable). --strict-credentials promotes credential hits from WARN to FAIL.",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:  "skip",
						Usage: "Skip the named probe (path | sudo | credentials). Repeatable.",
					},
					&cli.BoolFlag{
						Name:  "strict-credentials",
						Usage: "Fail (not warn) when a credential_env var is set in this session.",
					},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					opts := securityOptions{
						skips:             skipSet(cmd.StringSlice("skip")),
						strictCredentials: cmd.Bool("strict-credentials"),
					}
					return runSecurityCheck(os.Stdout, opts)
				},
			},
		},
	}
}

// runAllDoctorGroups runs every check group and writes their output to out.
// Returns a joined error when any group fails; partial output flushes either
// way so an operator sees what passed before the failure summary.
func runAllDoctorGroups(out io.Writer) error {
	var failures []string

	if summary, err := runAllowlistCheck(); err != nil {
		failures = append(failures, "allowlist: "+err.Error())
	} else {
		_, _ = fmt.Fprintln(out, summary)
	}

	if err := runSecurityCheck(out, securityOptions{}); err != nil {
		failures = append(failures, "security: "+err.Error())
	}

	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// securityOptions tunes ward doctor security. Zero value means "run every
// probe with default (warn-not-fail) credential semantics".
type securityOptions struct {
	skips             map[string]bool
	strictCredentials bool
}

// runSecurityCheck loads the resolved config, runs the host probes, writes
// per-row results to out, and returns a non-nil error when any FAIL row
// surfaced or when the policy declared protected binaries but every probe
// was skipped (the operator asked to silence the group).
func runSecurityCheck(out io.Writer, opts securityOptions) error {
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
