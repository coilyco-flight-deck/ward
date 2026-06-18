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
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/sandbox"
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
			upgradeCommand(),
			execCommand(),
			pkgCommand(),
			gitCommand(),
			auditCommand(),
			doctorCommand(),
			hookCommand(),
			installHooksCommand(),
			dispatchCommand(),
			containerCommand(),
			agentCommand(),
		},
	}

	// Unknown-verb fallback: `ward <leaf>` -> `ward exec <leaf>` for a declared
	// leaf that isn't a top-level verb. See docs/verb-fallback.md, issue #87.
	os.Args = maybeRewriteToExec(os.Args, topLevelVerbs(app))

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "ward:", err)
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		os.Exit(1)
	}
}

// rootValueFlags are root-level flags whose space-form value is the next token
// (so `ward --config x test` doesn't read x as the subcommand). See #87.
var rootValueFlags = map[string]bool{"--config": true}

// topLevelVerbs is the set of names cli dispatches directly: every registered
// command, its aliases, and the auto-added `help`. See docs/verb-fallback.md.
func topLevelVerbs(app *cli.Command) map[string]bool {
	verbs := map[string]bool{"help": true}
	for _, c := range app.Commands {
		verbs[c.Name] = true
		for _, alias := range c.Aliases {
			verbs[alias] = true
		}
	}
	return verbs
}

// firstSubcommandIndex returns the index of the first non-flag token after any
// root flags (and their space-form values), or -1 when there is none. See #87.
func firstSubcommandIndex(args []string) int {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return -1
		}
		if strings.HasPrefix(a, "-") {
			if rootValueFlags[a] && i+1 < len(args) {
				i++ // skip the flag's space-form value
			}
			continue
		}
		return i
	}
	return -1
}

// maybeRewriteToExec rewrites `ward <leaf> ...` to `ward exec <leaf> ...` for a
// declared, non-top-level leaf. Config loads only for unknown verbs. See #87.
func maybeRewriteToExec(args []string, topLevel map[string]bool) []string {
	idx := firstSubcommandIndex(args)
	if idx < 0 {
		return args
	}
	candidate := args[idx]
	if topLevel[candidate] {
		return args
	}
	cfg, err := loadDefault()
	if err != nil || cfg == nil {
		return args
	}
	for _, c := range cfg.Commands {
		if c.Name == candidate {
			rewritten := make([]string, 0, len(args)+1)
			rewritten = append(rewritten, args[:idx]...)
			rewritten = append(rewritten, "exec")
			rewritten = append(rewritten, args[idx:]...)
			return rewritten
		}
	}
	return args
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
