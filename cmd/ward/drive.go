package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
)

// drive.go wires `ward drive <harness>`, the surface `warded` shims onto
// (ward#247), and the flag boundary it owns (ward#248). See docs/drive.md.

// errNoHarness means no harness token was present: all ward flags, or empty.
var errNoHarness = errors.New("no harness named: want `ward drive [ward flags] <harness> [harness args]`")

// driveValueFlags are `drive` flags whose value is the next token in space form,
// so the harness-boundary scan skips that value. Long form only (see main.go).
var driveValueFlags = map[string]bool{"--policy": true}

// driveInvocation is the harness-boundary split: ward flags before the harness,
// the harness token, and the harness's own argv after it (verbatim).
type driveInvocation struct {
	WardArgs    []string
	Harness     string
	HarnessArgs []string
}

// splitDriveArgs draws the ward/harness flag boundary over the tokens after
// `ward drive` (ward#248). See docs/drive.md for the grammar it implements.
func splitDriveArgs(args []string) (driveInvocation, error) {
	var inv driveInvocation
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			// Terminator before any harness: the next token is the harness, the
			// remainder its raw argv.
			rest := args[i+1:]
			if len(rest) == 0 {
				return inv, errNoHarness
			}
			inv.Harness = rest[0]
			inv.HarnessArgs = append([]string{}, rest[1:]...)
			return inv, nil
		}
		if strings.HasPrefix(a, "-") {
			// A ward flag: carry it, plus its space-form value when it takes one.
			inv.WardArgs = append(inv.WardArgs, a)
			if driveValueFlags[a] && i+1 < len(args) {
				i++
				inv.WardArgs = append(inv.WardArgs, args[i])
			}
			continue
		}
		// First bare token: the harness.
		inv.Harness = a
		rest := args[i+1:]
		// Strip exactly one leading `--`: the passthrough marker. A second `--`
		// is the harness's own.
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		inv.HarnessArgs = append([]string{}, rest...)
		return inv, nil
	}
	return inv, errNoHarness
}

// driveWardFlags is the interpreted ward-level flag set for a `drive` call. The
// surface is small for ward#248; --policy is consumed by execution in ward#249.
type driveWardFlags struct {
	Policy string
	Help   bool
}

// parseDriveWardFlags interprets the ward flags before the harness. An unknown
// flag here is a boundary error (a harness flag belongs after the harness).
func parseDriveWardFlags(wardArgs []string) (driveWardFlags, error) {
	var f driveWardFlags
	for i := 0; i < len(wardArgs); i++ {
		a := wardArgs[i]
		switch {
		case a == "--help" || a == "-h":
			f.Help = true
		case a == "--policy":
			if i+1 >= len(wardArgs) {
				return f, fmt.Errorf("flag --policy needs a value")
			}
			i++
			f.Policy = wardArgs[i]
		case strings.HasPrefix(a, "--policy="):
			f.Policy = strings.TrimPrefix(a, "--policy=")
		default:
			return f, fmt.Errorf(
				"unknown ward flag %q before the harness: harness flags belong after "+
					"the harness name (or after `--`), ward flags before it", a)
		}
	}
	return f, nil
}

// driveUsage is the help block for a harness-less `ward drive` / `--help`.
// SkipFlagParsing turns off cli's auto --help, so drive renders its own.
const driveUsage = `ward drive - drive a harness behind ward's policy + audit boundary

usage:
  ward drive [ward flags] <harness> [harness flags...]
  warded     [ward flags] <harness> [harness flags...]   (warded is the public face)

the first bare token after drive is the harness. ward flags go before it,
the harness's own flags after it; use -- for explicit passthrough.

  warded --policy=strict gptme "deploy the thing"
  warded gptme -- --non-interactive "deploy the thing"

ward flags:
  --policy <name>   policy profile the harness runs under
  -h, --help        show this help

note: guarded container execution of the harness lands in ward#249. today
ward drive parses the invocation and prints the resolved plan.`

// renderDrivePlan formats the parsed invocation for the dry-run plan output.
func renderDrivePlan(inv driveInvocation, wf driveWardFlags) string {
	var b strings.Builder
	b.WriteString("ward drive (parsed plan):\n")
	if wf.Policy != "" {
		fmt.Fprintf(&b, "  policy:  %s\n", wf.Policy)
	}
	fmt.Fprintf(&b, "  harness: %s\n", inv.Harness)
	argv := append([]string{inv.Harness}, inv.HarnessArgs...)
	fmt.Fprintf(&b, "  argv:    %s\n", renderArgv(argv))
	b.WriteString("  (guarded execution is not yet wired; this prints the parsed invocation)")
	return b.String()
}

// renderArgv joins an argv for display, quoting any token with whitespace or
// quotes so the boundary split is legible at a glance.
func renderArgv(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\") {
			out[i] = strconv.Quote(a)
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

// driveCommand returns the `ward drive` verb (ward#247). SkipFlagParsing hands
// the Action every token after `drive` so splitDriveArgs owns the boundary.
func driveCommand() *cli.Command {
	return &cli.Command{
		Name:            "drive",
		Usage:           "Drive a harness behind ward's policy + audit boundary (public face: `warded`)",
		ArgsUsage:       "[ward flags] <harness> [harness flags...]",
		SkipFlagParsing: true,
		Description: "Run a headless harness (gptme, goose, ...) under ward. The first bare " +
			"token is the harness; ward flags go before it, the harness's own flags after " +
			"it, and `--` forces passthrough. The public face `warded` is a thin shim for " +
			"`ward drive`. Guarded container execution lands in ward#249; today this parses " +
			"the invocation and prints the resolved plan.",
		Action: func(_ context.Context, c *cli.Command) error {
			raw := c.Args().Slice()
			inv, err := splitDriveArgs(raw)
			if err != nil {
				// No harness: treat an explicit help request as help, else error.
				if wantsDriveHelp(raw) {
					fmt.Fprintln(c.Root().Writer, driveUsage)
					return nil
				}
				return err
			}
			wf, ferr := parseDriveWardFlags(inv.WardArgs)
			if ferr != nil {
				return ferr
			}
			if wf.Help {
				fmt.Fprintln(c.Root().Writer, driveUsage)
				return nil
			}
			fmt.Fprintln(c.Root().Writer, renderDrivePlan(inv, wf))
			return nil
		},
	}
}

// wantsDriveHelp reports whether harness-less args were an explicit help request
// (`ward drive`, `ward drive --help`) rather than a malformed call.
func wantsDriveHelp(raw []string) bool {
	if len(raw) == 0 {
		return true
	}
	for _, a := range raw {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}
