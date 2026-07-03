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
the only tap CI keeps fresh. If the installed keg's receipt lacks
source.full_name (a legacy/stale tab brew cannot canonicalize the formula
name to), the upgrade escalates to ` + "`brew reinstall`" + `, which matches
by rack directory and rewrites the receipt so the next upgrade self-heals.
See ward#581.

On Windows, where brew is absent, it runs the scoop sequence instead:

    scoop update
    scoop update ward  (detached; see below)

scoop refuses to overwrite an app whose executable is running, and the
` + "`ward upgrade`" + ` process is itself that ward.exe, so a direct
` + "`scoop update ward`" + ` self-blocks. ward sidesteps this by detaching the
app update to a background powershell that waits for this PID to exit, then
runs scoop with the lock gone; ` + "`ward upgrade`" + ` returns immediately
after dispatching (ward#568). The result lands in %TEMP%\ward-upgrade.log.

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

// upgradeScoopApp is the Windows scoop app name, matching ward.json in
// coilysiren/scoop-bucket. See ward#561.
const upgradeScoopApp = "ward"

// staleTabNotInstalled gates the reinstall escalation to the specific
// "<formula> not installed" signature a null-full_name receipt produces (ward#581).
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
	// Stale-receipt recovery: a null-full_name tab can't be matched from any
	// name (the ward#551 bare retry was inert), so reinstall by rack. See ward#581.
	if !staleTabNotInstalled(tail.String(), formula) {
		return fmt.Errorf("upgrade: brew upgrade %s: %w", formula, err)
	}
	fmt.Fprintln(os.Stderr, "==> brew reinstall", formula,
		"(stale receipt: keg tab lacks source.full_name, ward#581)")
	captured, err = r.execBrewRaw(ctx, []string{"reinstall", formula}, tail)
	*rows = append(*rows, captured...)
	if err != nil {
		return fmt.Errorf("upgrade: brew reinstall %s (stale-receipt recovery): %w", formula, err)
	}
	return nil
}

// runUpgradeScoop is runUpgrade's Windows arm: `scoop update` stays audited on
// the parent, `scoop update ward` detaches (ward#568). See the command Description.
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
	// `scoop update ward` refuses to overwrite the running ward.exe (this very
	// process), so detach it to a PID-waiter and return to free the exe. ward#568.
	pid := os.Getpid()
	name, args := scoopDetachArgv(scoopUpdateWardScript(pid, app))
	fmt.Fprintf(os.Stderr, "==> scoop update %s (detached: runs after ward.exe pid %d exits)\n", app, pid)
	if err := spawnDetachedScoop(name, args); err != nil {
		return fmt.Errorf("upgrade: dispatch detached scoop update %s: %w", app, err)
	}
	fmt.Fprintln(os.Stderr, `ward upgrade dispatched; the update finishes in the background once this process exits. Log: ward-upgrade.log in your TEMP directory.`)
	return nil
}

// scoopUpdateWardScript is the detached child's PowerShell: wait out the parent
// pid (bounded timeout), then `scoop update <app>`, logging both. See ward#568.
func scoopUpdateWardScript(pid int, app string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$log = Join-Path $env:TEMP 'ward-upgrade.log'
Add-Content -Path $log -Value "$(Get-Date -Format o) ward#568: waiting for ward.exe (pid %d) to exit"
Wait-Process -Id %d -Timeout 120
scoop update %s *>&1 | Add-Content -Path $log
Add-Content -Path $log -Value "$(Get-Date -Format o) ward#568: scoop update %s finished (exit $LASTEXITCODE)"
`, pid, pid, app, app)
}

// scoopDetachArgv wraps the update script in a hidden, profile-free powershell,
// split from the platform spawn so the argv is unit-testable off Windows. ward#568.
func scoopDetachArgv(script string) (string, []string) {
	return "powershell", []string{
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-Command", script,
	}
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
