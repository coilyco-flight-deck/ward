// Command ward is a contributor-facing cli-guard consumer entry point.
// See README.md for audience and scope.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/sandbox"
	"github.com/urfave/cli/v3"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// configFlagOverride is the explicit --config path captured at startup by
// preParseConfigFlag, before cli.Command construction sees it.
var configFlagOverride string

func explicitConfigPath() string { return configFlagOverride }

// sandboxShimSubcommand maps a wrapped-tool basename to the ward subcommand
// that re-enters the gate for it. Keep in sync with wardSandboxTools.
var sandboxShimSubcommand = map[string][]string{
	"brew": {"pkg", "brew"},
}

func main() {
	// Internal jail-helper re-exec, before normal CLI parsing; never returns on
	// success (it execs the real tool).
	if sandbox.IsJailInvocation(os.Args) {
		if err := sandbox.RunJail(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "ward:", err)
			os.Exit(1)
		}
		return
	}

	// Multicall shim: invoked under a wrapped tool's name (the jail masked it),
	// rewrite argv to re-enter the gate as `ward <subcommand> <args>`.
	if sub, ok := sandboxShimSubcommand[filepath.Base(os.Args[0])]; ok {
		rewritten := append([]string{"ward"}, sub...)
		os.Args = append(rewritten, os.Args[1:]...)
	}

	configFlagOverride = preParseConfigFlag(os.Args)
	app := &cli.Command{
		Name:    "ward",
		Usage:   "a contributor-facing cli-guard consumer",
		Version: Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "Path to a ward/coily yaml allowlist. Overrides cwd walk-up. $WARD_CONFIG is the env-var equivalent; --config wins.",
				Sources: cli.EnvVars("WARD_CONFIG"),
			},
			&cli.BoolFlag{
				Name: "audit-override-dirty",
				Usage: "Bypass the clean+synced tree gate on `ward exec` repo verbs. " +
					"Tags the audit row with audit_override=true and captures the " +
					"working tree status. For genuine emergencies only: the gate " +
					"exists so audit rows can be reconstructed from git history.",
			},
		},
		Commands: []*cli.Command{
			versionCommand(),
			execCommand(),
			pkgCommand(),
			gitCommand(),
			auditCommand(),
			doctorCommand(),
			hookCommand(),
			installHooksCommand(),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "ward:", err)
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		os.Exit(1)
	}
}

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print the build version and exit",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Println(Version)
			return nil
		},
	}
}
