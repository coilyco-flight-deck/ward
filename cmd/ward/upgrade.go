package main

import (
	"context"
	"fmt"
	"os"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
	"github.com/urfave/cli/v3"
)

// upgradeCommand is ward's self-update shorthand: the ward-native twin of
// `coily upgrade`, bound to the coilyco-flight-deck/ward/ward formula.
func upgradeCommand() *cli.Command {
	return &cli.Command{
		Name:  "upgrade",
		Usage: "Self-update via brew (coilyco-flight-deck/ward/ward per-repo tap).",
		Description: `upgrade runs the audited brew sequence:

    brew update
    brew upgrade coilyco-flight-deck/ward/ward

The formula is the per-repo tap coilyco-flight-deck/ward/ward. Pass --dry to
see the resolved version diff without installing (equivalent to
` + "`brew outdated coilyco-flight-deck/ward/ward`" + `).

Bare brew is denied at the lockdown layer; this verb is the audited
recovery path for an agent that needs a fresh ward binary. The
` + "`ward pkg brew`" + ` wrapper handles the general install/upgrade case for
any tap formula. ` + "`ward upgrade`" + ` is the ward-specific shortcut.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry",
				Usage: "show the resolved version diff without installing",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return newRunner().upgradeAction(ctx, c)
		},
	}
}

// upgradeAction wires the upgrade verb through ward's audit pipeline, capturing
// brew egress rows + the stderr tail onto the audit record like the pkg brew path.
func (r *Runner) upgradeAction(ctx context.Context, c *cli.Command) error {
	dry := c.Bool("dry")
	var rows []audit.EgressRow
	tail := newBrewTail()
	spec := verb.Spec{
		Name: "upgrade",
		ArgsFunc: func(_ *cli.Command) (map[string]string, []string) {
			return map[string]string{"--dry": fmt.Sprintf("%t", dry)}, nil
		},
		Action: func(ctx context.Context, _ *cli.Command) error {
			return r.runUpgrade(ctx, dry, &rows, tail)
		},
		OnComplete: makeBrewHook(&rows, tail),
	}
	return r.WrapVerb(spec, r.Audit)(ctx, c)
}

// upgradeFormula is the qualified formula ward's self-upgrade feeds `brew
// upgrade`; `ward pkg brew upgrade` is the path for any other formula.
const upgradeFormula = "coilyco-flight-deck/ward/ward"

// runUpgrade runs brew update + brew upgrade <formula> (or brew outdated under
// --dry), routing each brew call through ward's egress-proxied exec path.
func (r *Runner) runUpgrade(ctx context.Context, dry bool, rows *[]audit.EgressRow, tail *brewTail) error {
	formula := upgradeFormula
	if dry {
		fmt.Fprintln(os.Stderr, "==> brew outdated", formula)
		captured, err := r.execBrewRaw(ctx, []string{"outdated", formula}, tail)
		*rows = append(*rows, captured...)
		return err
	}
	fmt.Fprintln(os.Stderr, "==> brew update")
	captured, err := r.execBrewRaw(ctx, []string{"update"}, tail)
	*rows = append(*rows, captured...)
	if err != nil {
		return fmt.Errorf("upgrade: brew update: %w", err)
	}
	fmt.Fprintln(os.Stderr, "==> brew upgrade", formula)
	captured, err = r.execBrewRaw(ctx, []string{"upgrade", formula}, tail)
	*rows = append(*rows, captured...)
	if err != nil {
		return fmt.Errorf("upgrade: brew upgrade %s: %w", formula, err)
	}
	return nil
}
