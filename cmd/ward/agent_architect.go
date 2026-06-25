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

// agent_architect.go wires `ward agent architect`, the read-only interactive scoping
// role (ward#347, was explore; sandbox removed). See docs/agent-architect.md.

// architectSurface names the seedless interactive role for the shared bring-up's
// command line, container name, and audit verb.
const architectSurface = "architect"

// agentScratchFlags is the flag set the seedless scratch bring-up uses; architect is
// its only caller now that sandbox is gone (ward#347).
func agentScratchFlags() []cli.Flag {
	return []cli.Flag{
		agentDriverFlag(),
		&cli.StringFlag{Name: "repo", Usage: "owner/repo to clone for context (default: inferred from the cwd's git origin)"},
		&cli.StringSliceFlag{Name: "with-repo", Usage: "clone an additional repo for context (owner/name; repeatable), landed under /workspace alongside the primary repo (ward#230)."},
		&cli.StringFlag{Name: "image", Value: containerImageDefault, Sources: cli.EnvVars(envAgentImage), Usage: "dev-base image to run (env: WARD_AGENT_IMAGE)"},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Sources: cli.EnvVars(envAgentTag), Usage: "image tag (env: WARD_AGENT_TAG)"},
		&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
		&cli.StringFlag{Name: "ward-version", Sources: cli.EnvVars(envAgentVersion), Usage: "ward release the container downloads (default: this host's ward; env: WARD_AGENT_VERSION)"},
		&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
		hostNetFlag(),
		tsSidecarFlag(),
		&cli.BoolFlag{Name: "print", Usage: "resolve the repo + docker plan and exit; clone nothing, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	}
}

// agentArchitectCommand builds `ward agent architect`: a read-only interactive session
// that reads the repo but cannot push. Shares runScratchSession, readOnly=true (#347).
func agentArchitectCommand() *cli.Command {
	return &cli.Command{
		Name: "architect",
		Usage: "Drop into a read-only interactive agent in a fresh ephemeral container (repo clone + operating " +
			"context) with no issue and no seed - reads the repo and scopes + dispatches work, but cannot commit, push, or merge.",
		Flags: agentScratchFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentDriver(c)
			if err != nil {
				return fmt.Errorf("ward agent architect: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + "." + architectSurface,
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runScratchSession(ctx, cmd, mode, true)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runScratchSession is the seedless interactive bring-up architect uses; readOnly
// exports WARD_READONLY=1 (ward#293), revoking this clone's push. See agent-architect.md.
func (r *Runner) runScratchSession(ctx context.Context, c *cli.Command, mode containerMode, readOnly bool) error {
	label := agentCmdline(mode, architectSurface)

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
		// Architect keeps a dispatch-only capability: bind the docker socket so the
		// agent can commission a sealed sibling run (ward#315). docs/agent-architect.md.
		plan.Mounts = append(plan.Mounts, dockerSockMount())
	}
	// Name it ward-<repo>-architect-<mode>-<rand> so `docker ps` tells the run apart.
	plan.Name = fmt.Sprintf("%s-%s-%s-%s-%s", containerNamePrefix, safeRepoName(repo), architectSurface, mode, randHex())

	if c.Bool("print") {
		return printScratchPlan(c, plan, readOnly)
	}

	// Interactive dispatch: warn on a stale ward (ward#143), then reclaim dead
	// containers' layers so a busy fleet can't wedge new launches (ward#272).
	r.maybeWarnWardOutdated(ctx)
	r.sweepStaleContainers(ctx)
	if !c.Bool("no-pull") {
		r.pullAgentImage(ctx, plan, label)
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), r.resolveAgentCreds(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()
	access := "writable"
	if readOnly {
		access = "read-only"
	}
	fmt.Fprintf(os.Stderr, "%s: opening an interactive %s %s session on %s in a fresh container...\n\n", label, access, mode.agentBinary(), repo.slug())
	return r.createAgentContainer(ctx, plan, envFile)
}

// printScratchPlan renders the resolved repo + docker plan without cloning or firing
// - the dry-run preview for architect. There is no seed to show.
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
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(p.Mode, architectSurface))
	fmt.Fprintf(&b, "%s: agent runs interactive, attached, in a fresh ephemeral container (no seed)\n", architectSurface)
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
