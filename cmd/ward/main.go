// Command ward is a contributor-facing cli-guard consumer entry point.
// See README.md for audience and scope.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/urfave/cli/v3"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// configFlagOverride is the explicit --config path captured at startup by
// preParseConfigFlag, before cli.Command construction sees it.
var configFlagOverride string

func explicitConfigPath() string { return configFlagOverride }

func main() {
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
		},
		Commands: []*cli.Command{
			versionCommand(),
			execCommand(),
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
