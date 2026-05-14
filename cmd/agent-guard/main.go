// Command agent-guard is the generic-purpose cli-guard consumer entry point.
//
// Wraps a small fixed surface of dev verbs (build, test, vet, lint, tidy)
// behind cli-guard's policy gate. Intended for repos with external
// contributors where coily's Kai-specific verbs would be inappropriate.
// See README.md for the audience and scope rationale.
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
		Name:    "agent-guard",
		Usage:   "generic cli-guard consumer for external-contributor repos",
		Version: Version,
		Commands: []*cli.Command{
			versionCommand(),
			execCommand(),
			lintCommand(),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "agent-guard:", err)
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
