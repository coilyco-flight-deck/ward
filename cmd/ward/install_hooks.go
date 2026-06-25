package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/hook"
	"github.com/urfave/cli/v3"
)

// install_hooks.go is ward's `install-hooks` command surface; the settings.json
// merge mechanics are cli-guard's cli/hook.Installer. See docs/install-hooks.md.

// installHooksCommand returns the `install-hooks` subcommand. See docs/install-hooks.md.
func installHooksCommand() *cli.Command {
	return &cli.Command{
		Name:  "install-hooks",
		Usage: "Idempotently register the ward PreToolUse hook in .claude/settings.json.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Print the proposed write to stdout instead of writing.",
			},
			&cli.BoolFlag{
				Name:  "check",
				Usage: "Exit non-zero if the hook entry is not present. No writes.",
			},
			&cli.StringFlag{
				Name:  "path",
				Usage: "Explicit path to .claude/settings.json (default: <git-toplevel>/.claude/settings.json).",
			},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			return runInstallHooks(installHooksArgs{
				explicitPath: c.String("path"),
				dryRun:       c.Bool("dry-run"),
				check:        c.Bool("check"),
			}, os.Stdout)
		},
	}
}

type installHooksArgs struct {
	explicitPath string
	dryRun       bool
	check        bool
}

const (
	wantedMatcher = "Bash"
	wantedCommand = "ward hook pre-tool-use"
	wantedType    = "command"
)

// wardHookInstaller is the entry ward registers: the Bash PreToolUse hook that
// routes bare-binary invocations through `ward hook pre-tool-use`.
var wardHookInstaller = hook.Installer{Matcher: wantedMatcher, Command: wantedCommand, Type: wantedType}

func runInstallHooks(args installHooksArgs, out *os.File) error {
	target, err := hook.ResolveSettingsPath(args.explicitPath)
	if err != nil {
		return err
	}

	existing, err := hook.LoadSettings(target)
	if err != nil {
		return err
	}

	present, merged := wardHookInstaller.Ensure(existing)

	if args.check {
		if present {
			_, _ = fmt.Fprintf(out, "ward install-hooks: hook present at %s\n", target)
			return nil
		}
		return cli.Exit(fmt.Sprintf("ward install-hooks: hook not registered at %s", target), 1)
	}

	if present && !args.dryRun {
		_, _ = fmt.Fprintf(out, "ward install-hooks: hook already registered at %s, nothing to do\n", target)
		return nil
	}

	rendered, err := hook.MarshalSettings(merged)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if args.dryRun {
		_, _ = fmt.Fprintf(out, "ward install-hooks: would write to %s:\n", target)
		_, _ = out.Write(rendered)
		if !strings.HasSuffix(string(rendered), "\n") {
			_, _ = fmt.Fprintln(out)
		}
		return nil
	}

	if err := hook.WriteSettings(target, rendered); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	_, _ = fmt.Fprintf(out, "ward install-hooks: registered hook in %s\n", target)
	return nil
}
