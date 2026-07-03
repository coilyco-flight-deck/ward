package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/http/egress"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/audit"
	"github.com/urfave/cli/v3"
)

// upgradeCommand is ward's self-update shorthand: the ward-native twin of
// `coily upgrade`, bound to the coilyco-flight-deck/tap/ward formula.
func upgradeCommand() *cli.Command {
	return &cli.Command{
		Name:  "upgrade",
		Usage: "Self-update via brew (macOS/Linux) or scoop (Windows).",
		Description: `upgrade self-updates ward through the host's release channel.

On macOS/Linux it runs the audited brew sequence:

    brew update
    brew upgrade coilyco-flight-deck/tap/ward

The formula is the centralized flight-deck tap coilyco-flight-deck/tap/ward,
the only tap CI keeps fresh.

On Windows, where brew is absent, it runs the scoop sequence instead:

    scoop update
    scoop update ward

ward's scoop manifest lives in coilysiren/scoop-bucket, fed by the windows
` + "`.exe`" + ` + ` + "`.sha256`" + ` assets release.yml publishes per tag (ward#561).

Pass --dry to see the resolved version diff without installing (brew
outdated, or scoop status).

Bare brew/scoop is denied at the lockdown layer; this verb is the audited
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

// upgradeFormula names the centralized flight-deck tap (the only tap CI
// bumps); the in-repo Formula/ward.rb is frozen, not an upgrade source.
const upgradeFormula = "coilyco-flight-deck/tap/ward"

// upgradeFormulaBare is the unqualified fallback: it matches a null-full_name
// keg by rack dir where the tap-qualified name can't. See ward#551.
const upgradeFormulaBare = "ward"

// upgradeScoopApp is the Windows scoop app name, matching ward.json in
// coilysiren/scoop-bucket. Bare, like the brew bare-keg fallback. See ward#561.
const upgradeScoopApp = "ward"

// staleTabNotInstalled gates the unqualified retry to the specific "<formula>
// not installed" signature a null-full_name receipt produces. See ward#551.
func staleTabNotInstalled(tail, formula string) bool {
	return strings.Contains(tail, formula+" not installed")
}

// runUpgrade self-updates ward through the host's release channel: scoop on
// Windows (brew is absent there), brew on macOS/Linux. See ward#561.
func (r *Runner) runUpgrade(ctx context.Context, dry bool, rows *[]audit.EgressRow, tail *brewTail) error {
	if runtime.GOOS == "windows" {
		return r.runUpgradeScoop(ctx, dry, rows, tail)
	}
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
	if err == nil {
		return nil
	}
	// Stale-receipt recovery: retry unqualified when the tap-qualified name
	// can't match a null-full_name keg; other failures surface. See ward#551.
	if !staleTabNotInstalled(tail.String(), formula) {
		return fmt.Errorf("upgrade: brew upgrade %s: %w", formula, err)
	}
	fmt.Fprintln(os.Stderr, "==> brew upgrade", upgradeFormulaBare,
		"(fallback: tap-qualified keg not matched, ward#551)")
	captured, err = r.execBrewRaw(ctx, []string{"upgrade", upgradeFormulaBare}, tail)
	*rows = append(*rows, captured...)
	if err != nil {
		return fmt.Errorf("upgrade: brew upgrade %s (fallback %s): %w", formula, upgradeFormulaBare, err)
	}
	return nil
}

// runUpgradeScoop is runUpgrade's Windows arm: `scoop update` then `scoop update
// ward` (or `scoop status ward` under --dry). Brew's twin for scoop. See ward#561.
func (r *Runner) runUpgradeScoop(ctx context.Context, dry bool, rows *[]audit.EgressRow, tail *brewTail) error {
	app := upgradeScoopApp
	if dry {
		fmt.Fprintln(os.Stderr, "==> scoop status", app)
		captured, err := r.execScoopRaw(ctx, []string{"status", app}, tail)
		*rows = append(*rows, captured...)
		return err
	}
	fmt.Fprintln(os.Stderr, "==> scoop update")
	captured, err := r.execScoopRaw(ctx, []string{"update"}, tail)
	*rows = append(*rows, captured...)
	if err != nil {
		return fmt.Errorf("upgrade: scoop update: %w", err)
	}
	fmt.Fprintln(os.Stderr, "==> scoop update", app)
	captured, err = r.execScoopRaw(ctx, []string{"update", app}, tail)
	*rows = append(*rows, captured...)
	if err != nil {
		return fmt.Errorf("upgrade: scoop update %s: %w", app, err)
	}
	return nil
}

// execScoopRaw forwards `scoop <argv...>` under the egress proxy, the scoop twin
// of execBrewRaw (ModeObserve, no linuxbrew jail dance). See docs/release-binaries.md.
func (r *Runner) execScoopRaw(ctx context.Context, argv []string, tail *brewTail) ([]audit.EgressRow, error) {
	p := egress.New(nil, egress.ModeObserve)
	proxyURL, err := p.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("egress: start proxy: %w", err)
	}
	shadow := *r.Runner
	shadow.Env = append([]string(nil), r.Runner.Env...)
	shadow.Env = append(shadow.Env,
		"HTTPS_PROXY="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"https_proxy="+proxyURL,
		"http_proxy="+proxyURL,
	)
	if r.Runner.Stderr != nil {
		shadow.Stderr = io.MultiWriter(r.Runner.Stderr, tail)
	} else {
		shadow.Stderr = tail
	}
	execErr := shadow.Exec(ctx, "scoop", argv...)
	rows := p.Stop()
	return rows, execErr
}
