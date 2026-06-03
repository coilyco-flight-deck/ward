package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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
				Usage: "Summarize the parsed security: block from the resolved config.",
				Action: func(_ context.Context, _ *cli.Command) error {
					summary, err := runSecurityCheck()
					if err != nil {
						return err
					}
					fmt.Println(summary)
					return nil
				},
			},
		},
	}
}

// runAllDoctorGroups runs every check group and prints a per-group summary
// to out. Returns a joined error when any group fails; the partial summary
// is still flushed so an operator sees what passed.
func runAllDoctorGroups(out *os.File) error {
	var summaries []string
	var failures []string

	if summary, err := runAllowlistCheck(); err != nil {
		failures = append(failures, "allowlist: "+err.Error())
	} else {
		summaries = append(summaries, summary)
	}

	if summary, err := runSecurityCheck(); err != nil {
		failures = append(failures, "security: "+err.Error())
	} else {
		summaries = append(summaries, summary)
	}

	for _, s := range summaries {
		_, _ = fmt.Fprintln(out, s)
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// runSecurityCheck loads the resolved config and reports on its security:
// block. Returns a one-line summary on success. A zero-value Security block
// is a pass and reports "no security: declared".
func runSecurityCheck() (string, error) {
	cfg, err := loadDefault()
	if err != nil {
		return "", err
	}
	return summarizeSecurity(cfg.Security), nil
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
