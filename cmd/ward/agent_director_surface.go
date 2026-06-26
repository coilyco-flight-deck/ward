package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

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
	flags := []cli.Flag{
		agentDriverFlag(),
		&cli.StringFlag{Name: "repo", Usage: "owner/repo to clone for context (default: inferred from the cwd's git origin)"},
		&cli.StringSliceFlag{Name: "with-repo", Usage: "clone an additional repo for context (owner/name; repeatable), landed under /workspace alongside the primary repo (ward#230)."},
	}
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
			mode, err := agentDriver(c)
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

	// The context repo is --repo, else inferred from the cwd's git origin (the
	// same target resolution the container bring-up uses).
	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: a bypassPermissions clone of private code, so only act on an owner
	// in the primary-org set - the same gate the engineer + advisor roles apply.
	if !r.ownerAllowed(repo.Owner) {
		return fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, repo.Owner, strings.Join(r.primaryOrgs(), ", "))
	}

	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return err
	}
	// The session always runs attached and ephemeral, so its assets clean up on return.
	defer cleanupAssets()

	// No seed: empty AgentArgs is the bare interactive bring-up, so the entrypoint
	// launches a plain agent REPL (claude / goose session / codex / opencode).
	plan, err := buildUpPlan(c, repo, mode, cwd, assetsDir, nil)
	if err != nil {
		return err
	}
	plan.ReadOnly = readOnly
	if readOnly {
		// Keep a dispatch-only capability: bind the docker socket so the agent can
		// commission a sealed sibling run (ward#315). docs/agent-surface.md.
		plan.Mounts = append(plan.Mounts, dockerSockMount())
	}
	// Name it session-<driver>-<machine> (issueless, so the machine id disambiguates
	// concurrent surface sessions) and label ward.role=session (ward#364, ward#353).
	plan.Role = roleSession
	plan.Name = containerRoleName(roleSession, mode, repo, 0, plan.Machine)

	if c.Bool("print") {
		return printScratchPlan(c, plan, readOnly)
	}

	// Interactive dispatch: reclaim dead containers' layers so a busy fleet can't
	// wedge new launches (ward#272). The stale-ward heads-up (ward#143) rides the gate.
	r.sweepStaleContainers(ctx)
	if !c.Bool("no-pull") {
		r.pullAgentImage(ctx, plan, label)
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), r.resolveAgentCreds(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()

	// Pre-launch gate before the fullscreen TUI (ward#366); see docs/agent-gate.md.
	// proceed=false means an upgrade re-launch superseded this process's launch.
	proceed, err := r.runScratchGate(ctx, c, plan, readOnly, label)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	access := "writable"
	if readOnly {
		access = "read-only"
	}
	fmt.Fprintf(os.Stderr, "%s: opening an interactive %s %s session on %s in a fresh container...\n\n", label, access, mode.agentBinary(), repo.slug())
	return r.createAgentContainer(ctx, plan, envFile)
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
		access = "read-only (this clone's push wiring revoked; dispatch token + docker socket kept)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(p.Mode, directorSurfaceVerb))
	fmt.Fprintf(&b, "%s: agent runs interactive, attached, in a fresh ephemeral container (no seed)\n", directorSurfaceVerb)
	fmt.Fprintf(&b, "access: %s\n", access)
	fmt.Fprintf(&b, "repo:   %s\n", p.Repo.slug())
	fmt.Fprintf(&b, "name:   %s\n", p.Name)
	if c.Bool("no-pull") {
		fmt.Fprintf(&b, "# pull skipped (--no-pull); image: %s\n", p.Image)
	} else {
		fmt.Fprintf(&b, "docker pull %s\n", p.Image)
	}
	fmt.Fprintf(&b, "docker %s\n", strings.Join(dockerCreateArgv(p, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, b.String())
	return err
}
