package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
	"github.com/urfave/cli/v3"
)

// eco.go wires the `ward eco` verb surface: the content-pipeline gate + guarded
// auto-promote for Kai's Eco server (ward#582). See docs/eco-test.md.

const (
	envEcoServerDir   = "WARD_ECO_SERVER_DIR"    // local Steam EcoServer install root (Windows)
	envEcoKaiHost     = "WARD_ECO_KAI_HOST"      // ssh target for the live kai-server
	envEcoRemoteDir   = "WARD_ECO_REMOTE_DIR"    // EcoServer root on kai-server
	envEcoScriptsDir  = "WARD_ECO_SCRIPTS_DIR"   // infra scripts dir on kai-server
	envEcoSnapshotDir = "WARD_ECO_SNAPSHOT_ROOT" // snapshot root on kai-server

	// defaultRemoteDir mirrors scripts/eco-server-start.sh in the infra repo.
	defaultRemoteDir  = "/home/kai/Steam/steamapps/common/EcoServer"
	defaultScriptsDir = "/home/kai/projects/infrastructure/scripts"
)

// ecoCommand is the `eco` umbrella. Today it carries the single `test` role; the
// legacy `coily eco mod push`/`restart` verbs migrate under here later (ward#582).
func ecoCommand() *cli.Command {
	return &cli.Command{
		Name:  "eco",
		Usage: "Eco content pipeline: smoke-gate a mod change locally, then guardedly auto-promote to kai-server.",
		Description: `eco carries the Eco content pipeline (eco-ops#30). 'ward eco test' boots a
throwaway EcoServer with your working-copy mod/config change on the local Steam
install and runs a fail-closed smoke gate: the server must reach ready with NO
ModKit load exception (eco-ops#7) and no UserCode compile failure. On a green
gate, --promote drives the guarded live-side promote to kai-server:

  snapshot Mods/+Configs/ -> apply -> restart -> health probe -> canary watch
      -> healthy (done) | degraded (auto-rollback + restart, fail loud)

The live world only ever ends green or reverted. Without --promote the gate runs
alone (safe to run anytime). --dry-run prints the plan and touches nothing.

  ward eco test --mod EcoTelemetry              # gate the working-copy change, no promote
  ward eco test --mod EcoTelemetry --promote    # gate, then auto-promote on green
  ward eco test --promote --dry-run             # show the boot + promote plan, run nothing

Config is env/flag driven: WARD_ECO_SERVER_DIR (local install), WARD_ECO_KAI_HOST
(ssh target), WARD_ECO_REMOTE_DIR, WARD_ECO_SCRIPTS_DIR, WARD_ECO_SNAPSHOT_ROOT.
See docs/eco-test.md.`,
		Commands: []*cli.Command{
			ecoTestCommand(),
		},
	}
}

// ecoTestCommand builds `ward eco test`.
func ecoTestCommand() *cli.Command {
	return &cli.Command{
		Name:  "test",
		Usage: "Boot a throwaway EcoServer with the working-copy change and run the smoke gate (optionally auto-promote).",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server-dir",
				Sources: cli.EnvVars(envEcoServerDir),
				Usage:   "local Steam EcoServer install root to boot the throwaway world from (env: " + envEcoServerDir + ")",
			},
			&cli.StringSliceFlag{
				Name:  "mod",
				Usage: "name of a mod the change touches; each must report loaded in the boot log (repeatable)",
			},
			&cli.StringFlag{
				Name:  "mod-src",
				Usage: "path to the working-copy mod/config to stage into the throwaway world (default: cwd)",
			},
			&cli.BoolFlag{
				Name:  "promote",
				Usage: "on a green gate, auto-promote to the live kai-server (snapshot+health+canary+rollback). Off by default: the first real promote is Kai's.",
			},
			&cli.BoolFlag{
				Name:  flagDryRun,
				Usage: "print the boot + promote plan and exit; boot nothing, touch no live server",
			},
			&cli.DurationFlag{
				Name:  "boot-timeout",
				Value: 5 * time.Minute,
				Usage: "how long to wait for the throwaway server to reach ready before failing the gate",
			},
			&cli.IntFlag{
				Name:  "canary-samples",
				Value: 3,
				Usage: "post-restart health samples to take during the canary watch window",
			},
			&cli.DurationFlag{
				Name:  "canary-interval",
				Value: 20 * time.Second,
				Usage: "wait between canary samples",
			},
			&cli.StringFlag{
				Name:    "kai-host",
				Sources: cli.EnvVars(envEcoKaiHost),
				Usage:   "ssh target for the live kai-server promote (env: " + envEcoKaiHost + ")",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return newRunner().runEcoTest(ctx, cmd)
		},
	}
}

// ecoTestOptions is the parsed, validated invocation.
type ecoTestOptions struct {
	serverDir    string
	mods         []string
	modSrc       string
	promote      bool
	dryRun       bool
	bootTimeout  time.Duration
	kaiHost      string
	remoteDir    string
	scriptsDir   string
	snapshotRoot string
	plan         promotePlan
}

// ecoTestOptionsFromCmd reads flags + env into a normalized options struct.
func ecoTestOptionsFromCmd(cmd *cli.Command) ecoTestOptions {
	modSrc := cmd.String("mod-src")
	if strings.TrimSpace(modSrc) == "" {
		if cwd, err := os.Getwd(); err == nil {
			modSrc = cwd
		}
	}
	return ecoTestOptions{
		serverDir:    strings.TrimSpace(cmd.String("server-dir")),
		mods:         cmd.StringSlice("mod"),
		modSrc:       modSrc,
		promote:      cmd.Bool("promote"),
		dryRun:       cmd.Bool(flagDryRun),
		bootTimeout:  cmd.Duration("boot-timeout"),
		kaiHost:      strings.TrimSpace(cmd.String("kai-host")),
		remoteDir:    envOr(envEcoRemoteDir, defaultRemoteDir),
		scriptsDir:   envOr(envEcoScriptsDir, defaultScriptsDir),
		snapshotRoot: strings.TrimSpace(os.Getenv(envEcoSnapshotDir)),
		plan: promotePlan{
			CanarySamples:  cmd.Int("canary-samples"),
			CanaryInterval: cmd.Duration("canary-interval"),
		},
	}
}

// runEcoTest is the top-level `ward eco test` action: gate, then optional
// promote. It exits non-zero (blocking any promote) on a failed gate.
func (r *Runner) runEcoTest(ctx context.Context, cmd *cli.Command) error {
	opts := ecoTestOptionsFromCmd(cmd)

	if opts.dryRun {
		printEcoDryRun(opts)
		return nil
	}

	if opts.serverDir == "" {
		return fmt.Errorf("ward eco test: no local EcoServer install configured; set --server-dir or %s", envEcoServerDir)
	}

	// 1. Boot the throwaway world and capture its log, then run the pure gate.
	fmt.Printf("eco test: booting throwaway EcoServer from %s\n", opts.serverDir)
	log, bootErr := r.bootLocalSmoke(ctx, opts)
	gate := analyzeSmoke(log, opts.mods, defaultSmokeSignals())
	fmt.Println(gate.summaryLine())
	if bootErr != nil {
		// A boot that crashed/exited non-zero blocks promote regardless of the
		// log verdict - the server did not stay up.
		return exitcode.New(exitcode.UpstreamFailed, "eco_boot_failed",
			fmt.Errorf("ward eco test: throwaway boot failed: %w", bootErr), "")
	}
	if !gate.Pass {
		for _, reason := range gate.Reasons {
			fmt.Printf("  - %s\n", reason)
		}
		return exitcode.New(exitcode.Generic, "eco_gate_blocked",
			fmt.Errorf("ward eco test: smoke gate blocked promote"), "")
	}

	if !opts.promote {
		fmt.Println("eco test: gate green; --promote not set, stopping before live promote")
		return nil
	}

	// 2. Green gate + --promote: drive the guarded live-side promote.
	if opts.kaiHost == "" {
		return fmt.Errorf("ward eco test: --promote needs a live target; set --kai-host or %s", envEcoKaiHost)
	}
	exec := r.newEcoSSHExecutor(opts)
	fmt.Printf("eco test: gate green - promoting to %s\n", opts.kaiHost)
	res, err := runPromote(ctx, opts.plan, exec, func(f string, a ...any) {
		fmt.Printf("  "+f+"\n", a...)
	}, time.Sleep)
	if err != nil {
		return fmt.Errorf("ward eco test: promote errored: %w", err)
	}
	fmt.Println(res.summaryLine())
	if !res.ok() {
		// Rollback failed: the live world is in an unknown state. Fail loud.
		return exitcode.New(exitcode.Internal, "eco_rollback_failed",
			fmt.Errorf("ward eco test: %s", res.summaryLine()), "manual intervention required on kai-server")
	}
	if res.Outcome == outcomeRolledBack || res.Outcome == outcomeAborted {
		// A clean revert / untouched abort is still a non-success for the change.
		return exitcode.New(exitcode.Generic, "eco_not_promoted",
			fmt.Errorf("ward eco test: change not promoted (%s)", res.Outcome), "")
	}
	return nil
}

// printEcoDryRun renders the full boot + promote plan without side effects.
func printEcoDryRun(opts ecoTestOptions) {
	fmt.Println("ward eco test --dry-run (no boot, no live changes)")
	fmt.Printf("  local server-dir: %s\n", orUnset(opts.serverDir))
	fmt.Printf("  mod-src:          %s\n", orUnset(opts.modSrc))
	fmt.Printf("  required mods:    %s\n", orUnset(strings.Join(opts.mods, ", ")))
	fmt.Printf("  boot timeout:     %s\n", opts.bootTimeout)
	fmt.Println("  gate: boot throwaway world, require ready + no ModKit exception + no compile failure")
	if !opts.promote {
		fmt.Println("  promote: DISABLED (pass --promote to auto-promote on a green gate)")
		return
	}
	fmt.Printf("  promote target:   %s:%s (via %s)\n", orUnset(opts.kaiHost), opts.remoteDir, opts.scriptsDir)
	fmt.Println("  promote steps (only if gate green):")
	for _, line := range dryRunPlanLines(opts.plan) {
		fmt.Printf("    %s\n", line)
	}
}

// orUnset renders "(unset)" for an empty string, for readable dry-run output.
func orUnset(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(unset)"
	}
	return s
}
