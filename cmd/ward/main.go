// Command ward is coilysiren's contributor-facing cli-guard consumer entry point.
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

func main() {
	app := &cli.Command{
		Name:    "ward",
		Usage:   "coilysiren's contributor-facing cli-guard consumer",
		Version: Version,
		Commands: []*cli.Command{
			versionCommand(),
			execCommand(),
			lintCommand(),
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
