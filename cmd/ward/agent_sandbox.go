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

// agent_sandbox.go wires `ward agent sandbox`: the writable seedless interactive
// scratch session. Shares runScratchSession with `explore`. See docs/agent-sandbox.md.

// agentScratchFlags is the flag set the two seedless scratch surfaces (sandbox +
// explore) share; they differ only in whether the session can push.
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
		&cli.BoolFlag{Name: "print", Usage: "resolve the repo + docker plan and exit; clone nothing, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	}
}

// agentSandboxCommand builds `ward agent sandbox`: a writable seedless scratch
// session, the interactive sibling of `ask` (ward#292).
func agentSandboxCommand() *cli.Command {
	return &cli.Command{
		Name: "sandbox",
		Usage: "Drop into an interactive agent in a fresh ephemeral container (repo clone + operating context) " +
			"with no issue and no seed - an unguided, writable scratch session.",
		Flags: agentScratchFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentDriver(c)
			if err != nil {
				return fmt.Errorf("ward agent sandbox: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".sandbox",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runScratchSession(ctx, cmd, mode, false)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// scratchSurface names the seedless interactive surface a run came in on, so the
// shared bring-up labels its command line, container name, and audit verb right.
func scratchSurface(readOnly bool) string {
	if readOnly {
		return "explore"
	}
	return "sandbox"
}

// runScratchSession is the seedless interactive bring-up `sandbox` and `explore`
// share; readOnly exports WARD_READONLY=1 (ward#293). See docs/agent-explore.md.
func (r *Runner) runScratchSession(ctx context.Context, c *cli.Command, mode containerMode, readOnly bool) error {
	surface := scratchSurface(readOnly)
	label := agentCmdline(mode, surface)

	// The context repo is --repo, else inferred from the cwd's git origin (the
	// same target resolution ask + the container bring-up use).
	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: a bypassPermissions clone of private code, so only act on an owner
	// in the primary-org set - the same gate work/task/ask apply (read-only or not).
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
	// Name it ward-<repo>-<surface>-<mode>-<rand> so `docker ps` tells the run apart.
	plan.Name = fmt.Sprintf("%s-%s-%s-%s-%s", containerNamePrefix, safeRepoName(repo), surface, mode, randHex())

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
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, r.resolveAgentCreds(ctx, mode))
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

// printScratchPlan renders the resolved repo + docker plan without cloning or
// firing - the dry-run preview for sandbox + explore. There is no seed to show.
func printScratchPlan(c *cli.Command, p upPlan, readOnly bool) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	access := "writable"
	if readOnly {
		access = "read-only (push credential revoked after clone)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(p.Mode, scratchSurface(readOnly)))
	fmt.Fprintf(&b, "%s: agent runs interactive, attached, in a fresh ephemeral container (no seed)\n", scratchSurface(readOnly))
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
