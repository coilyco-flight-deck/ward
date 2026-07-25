package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_director_surface.go is the director's read-only interactive surface phase: the
// seedless bring-up it drops into on drain (ward#353). See docs/agent-surface.md.

// directorSurfaceVerb names the surface for its command line, name, and audit verb.
// Internal: `warded surface` is not registered, only the director reaches it.
const directorSurfaceVerb = "surface"

// agentScratchFlags is the flag set the seedless surface bring-up uses; the director's
// surface phase is its only caller now the standalone architect is gone (ward#353).
func agentScratchFlags() []cli.Flag {
	flags := agentHarnessFlags()
	flags = append(flags,
		&cli.StringFlag{Name: "repo", Usage: "owner/repo to clone for context (default: inferred from the cwd's git origin)"},
		&cli.StringSliceFlag{Name: "with-repo", Usage: "clone an additional repo for context (owner/name; repeatable), landed at /workspace/<owner>/<repo> (ward#1526)."},
	)
	flags = append(flags, agentImageFlags()...)
	return append(flags,
		&cli.BoolFlag{Name: "print", Usage: "resolve the repo + docker plan and exit; clone nothing, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	)
}

// directorSurfaceCommand builds the surface as an internal, unregistered command the
// director runs via directorSurfaceArgv; `warded surface` errors as unknown (ward#353).
func directorSurfaceCommand() *cli.Command {
	return &cli.Command{
		Name: directorSurfaceVerb,
		Usage: "The director's read-only interactive surface (internal): a fresh ephemeral container (repo clone + " +
			"operating context) with no issue and no seed - reads the repo and scopes + dispatches work, but cannot commit, push, or merge.",
		Flags: agentScratchFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentHarness(c)
			if err != nil {
				return fmt.Errorf("ward agent %s: %w", directorSurfaceVerb, err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + "." + directorSurfaceVerb,
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runScratchSession(ctx, cmd, mode, true)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runScratchSession is the seedless interactive bring-up the surface phase uses; readOnly
// exports WARD_READONLY=1 (ward#293). See docs/agent-surface.md.
func (r *Runner) runScratchSession(ctx context.Context, c *cli.Command, mode containerMode, readOnly bool) error {
	label := agentCmdline(mode, directorSurfaceVerb)
	plan, cleanupAssets, err := r.prepareScratchPlan(ctx, c, mode, readOnly, label)
	if err != nil {
		return err
	}
	// Writable scratch sessions remain ephemeral. Read-only director stacks keep
	// their assets under ~/.ward so the broker can outlive this process.
	defer cleanupAssets()

	if c.Bool("print") {
		if readOnly {
			plan.DispatchBrokerAddr = dispatchBrokerServiceAddress
			plan.DispatchBrokerToken = "<dispatch-broker-token>"
		}
		return printScratchPlan(c, plan, readOnly)
	}

	// Ready the tailnet network, sweep dead containers, then pull - the shared
	// pre-launch steps; a missing ward-tailnet network is created here (ward#597, #272).
	if err := r.prelaunchDispatch(ctx, c, plan, label); err != nil {
		return err
	}
	var launchCreds []agentsapi.EnvLine
	if readOnly {
		launchCreds = r.resolveDirectorStackCreds(ctx, &plan, mode)
	} else {
		launchCreds = r.resolveLaunchCreds(ctx, &plan, mode)
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), plan.Forge, launchCreds)
	if err != nil {
		return err
	}
	defer cleanupEnv()

	// Pre-launch gate before the fullscreen TUI (ward#366); see docs/agent-gate.md.
	// proceed=false means an upgrade re-launch superseded this process's launch.
	r.runScratchGate(ctx, plan, readOnly)

	access := "writable"
	if readOnly {
		access = "read-only"
	}
	fmt.Fprintf(os.Stderr, "%s: opening an interactive %s %s session on %s in a fresh container...\n\n", label, access, lookupAgent(mode).Record().Binary, plan.Repo.slug())
	if readOnly {
		stack, serr := resolveDirectorStack(plan.Repo, plan.Mode)
		if serr != nil {
			return fmt.Errorf("%s: resolve director stack: %w", label, serr)
		}
		return r.runDirectorStack(ctx, plan, stack, envFile)
	}
	return r.createAgentContainer(ctx, plan, envFile)
}

func (r *Runner) prepareScratchPlan(ctx context.Context, c *cli.Command, mode containerMode, readOnly bool, label string) (upPlan, func(), error) {
	// The context repo is --repo, else inferred from the cwd's git origin (the
	// same target resolution the container bring-up uses).
	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return upPlan{}, func() {}, fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: a bypassPermissions clone of private code, so only act on an owner
	// in the trusted-owner set - the same gate the engineer role applies.
	if !r.ownerAllowed(repo.Owner) {
		return upPlan{}, func() {}, r.untrustedOwnerErr(label, repo.Owner)
	}
	var assetsDir string
	var cleanupAssets func()
	if readOnly {
		stack, serr := prepareDirectorStackAssets(ctx, repo, mode, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
		if serr != nil {
			return upPlan{}, func() {}, serr
		}
		assetsDir = stack.AssetsDir
		cleanupAssets = func() {}
	} else {
		assetsDir, cleanupAssets, err = writeContainerAssets(ctx, r, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
		if err != nil {
			return upPlan{}, func() {}, err
		}
	}
	// No seed: empty AgentArgs is the bare interactive bring-up (a plain agent REPL). The
	// read-only surface opts into the host agent-log drain mount (ward#525).
	plan, err := buildUpPlan(c, repo, mode, roleDirector, cwd, assetsDir, nil, readOnly)
	if err != nil {
		cleanupAssets()
		return upPlan{}, func() {}, err
	}
	plan.ReadOnly = readOnly
	if readOnly {
		normalizeDirectorStackNetwork(&plan)
		plan.DispatchBrokerAddr = dispatchBrokerServiceAddress
	}

	return plan, cleanupAssets, nil
}

// printScratchPlan renders the resolved repo + docker plan without cloning or firing
// - the dry-run preview for the surface session. There is no seed to show.
func printScratchPlan(c *cli.Command, p upPlan, readOnly bool) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	access := "writable"
	if readOnly {
		access = "read-only (this clone's push wiring revoked; supervised broker reachable at " + dispatchBrokerServiceAddress + ")"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(p.Mode, directorSurfaceVerb))
	fmt.Fprintf(&b, "%s: agent runs interactive, attached, in a fresh ephemeral container (no seed)\n", directorSurfaceVerb)
	fmt.Fprintf(&b, "access: %s\n", access)
	fmt.Fprintf(&b, "repo:   %s\n", p.Repo.slug())
	fmt.Fprintf(&b, "name:   %s\n", p.Name)
	if c.Bool("no-pull") {
		fmt.Fprintf(&b, "# pull skipped (--no-pull). image: %s\n", p.Image)
	} else {
		fmt.Fprintf(&b, "docker pull %s\n", p.Image)
	}
	if readOnly {
		stack, err := resolveDirectorStack(p.Repo, p.Mode)
		if err != nil {
			return err
		}
		up, run := directorStackComposeArgs(p, stack)
		fmt.Fprintf(&b, "docker %s\n", strings.Join(up, " "))
		fmt.Fprintf(&b, "docker %s\n", strings.Join(run, " "))
		_, err = io.WriteString(out, b.String())
		return err
	}
	fmt.Fprintf(&b, "docker %s\n", strings.Join(dockerCreateArgv(p, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, b.String())
	return err
}
