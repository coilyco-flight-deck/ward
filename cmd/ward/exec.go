package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/policy"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
	"github.com/urfave/cli/v3"
)

// execCommand returns the `exec` verb. See docs/exec-verb.md.
func execCommand() *cli.Command {
	cfg, loadErr := loadDefault()
	if loadErr != nil || cfg == nil || len(cfg.Commands) == 0 {
		return &cli.Command{
			Name:  "exec",
			Usage: "Run a named command from .ward/ward.yaml (no config reachable)",
			Action: func(_ context.Context, _ *cli.Command) error {
				if loadErr != nil {
					return loadErr
				}
				return errNoConfig
			},
		}
	}
	subs := make([]*cli.Command, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		subs = append(subs, buildExecLeaf(cfg, c))
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name < subs[j].Name })
	repoRoot := filepath.Dir(filepath.Dir(cfg.Path))
	return &cli.Command{
		Name:     "exec",
		Usage:    "Run a command declared in " + cfg.Path,
		Commands: subs,
		Description: fmt.Sprintf(
			"Per-repo command declared in %s. Expands to a pre-validated argv "+
				"and runs with cwd set to %s. Every argv token is checked against "+
				"cli-guard's shell-metacharacter policy before execve.",
			cfg.Path, repoRoot,
		),
	}
}

// buildExecLeaf wraps a single command from the config into a cli.Command
// whose Action runs the configured argv inside the repo that declared it.
func buildExecLeaf(cfg *repocfg.Config, rc repocfg.Command) *cli.Command {
	repoRoot := filepath.Dir(filepath.Dir(cfg.Path))
	usage := rc.Description
	if usage == "" {
		usage = "Run: " + strings.Join(rc.Argv, " ")
	}
	return &cli.Command{
		Name:      rc.Name,
		Usage:     usage,
		ArgsUsage: "[-- extra args]",
		Description: fmt.Sprintf(
			"Per-repo command declared in %s.\nExpands to: %s\nRuns in: %s",
			cfg.Path, strings.Join(rc.Argv, " "), repoRoot,
		),
		Action: func(ctx context.Context, c *cli.Command) error {
			extras := c.Args().Slice()
			if err := policy.ValidateArgSlice("positional", extras); err != nil {
				return fmt.Errorf("argv rejected by cli-guard policy: %w", err)
			}
			argv := append([]string{}, rc.Argv...)
			argv = append(argv, extras...)
			fmt.Fprintf(os.Stderr, "ward: exec %s in %s\n", rc.Name, repoRoot)
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- argv is loaded from the policy-validated repo allowlist
			cmd.Dir = repoRoot
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			return cmd.Run()
		},
	}
}
