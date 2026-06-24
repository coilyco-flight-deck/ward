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

// agent_sandbox.go wires `ward agent sandbox`: drop into an interactive agent in
// a fresh container with no issue and no seed - the unguided scratch session.
// See docs/agent-sandbox.md.

// agentSandboxCommand builds `ward agent sandbox` so a human can poke around in a
// fresh clone + operating context with a live agent, carrying no issue (ward#292).
// It is the interactive sibling of `ask`: same container, no seed, no one-shot.
func agentSandboxCommand() *cli.Command {
	return &cli.Command{
		Name: "sandbox",
		Usage: "Drop into an interactive agent in a fresh ephemeral container (repo clone + operating context) " +
			"with no issue and no seed - an unguided, writable scratch session.",
		Flags: []cli.Flag{
			agentDriverFlag(),
			&cli.StringFlag{Name: "repo", Usage: "owner/repo to clone for context (default: inferred from the cwd's git origin)"},
			&cli.StringSliceFlag{Name: "with-repo", Usage: "clone an additional repo for context (owner/name; repeatable), landed under /workspace alongside the primary repo (ward#230)."},
			&cli.StringFlag{Name: "image", Value: containerImageDefault, Usage: "dev-base image to run"},
			&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Usage: "image tag"},
			&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
			&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
			&cli.BoolFlag{Name: "print", Usage: "resolve the repo + docker plan and exit; clone nothing, run nothing"},
			&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
		},
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
					return r.runAgentSandbox(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentSandbox resolves the context repo, spins up a fresh attached container
// with no seed, and drops the human into the interactive agent. The session is
// writable (it gets the push token like `work`), so it can commit/merge/push;
// the only difference from a carry is that nothing is assigned. See docs/agent-sandbox.md.
func (r *Runner) runAgentSandbox(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "sandbox")

	// The context repo is --repo, else inferred from the cwd's git origin (the
	// same target resolution ask + the container bring-up use).
	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: sandbox spins a bypassPermissions container with a push token and
	// clones the repo, so only act on an owner in the primary-org set - the same
	// gate work/task/ask apply.
	if !r.ownerAllowed(repo.Owner) {
		return fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, repo.Owner, strings.Join(r.primaryOrgs(), ", "))
	}

	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return err
	}
	// sandbox always runs attached and ephemeral, so its assets clean up on return.
	defer cleanupAssets()

	// No seed: empty AgentArgs is the bare interactive bring-up, so the entrypoint
	// launches a plain agent REPL (claude / goose session / codex / opencode).
	plan, err := buildUpPlan(c, repo, mode, cwd, assetsDir, nil)
	if err != nil {
		return err
	}
	// Name it ward-<repo>-sandbox-<mode>-<rand> so `docker ps` tells a sandbox run
	// apart from a carry run or an ask run.
	plan.Name = fmt.Sprintf("%s-%s-sandbox-%s-%s", containerNamePrefix, safeRepoName(repo), mode, randHex())

	if c.Bool("print") {
		return printAgentSandboxPlan(c, plan)
	}

	// sandbox is interactive (a human is at the terminal), so this dispatch is the
	// moment to surface a stale-ward reminder before the container spins (ward#143).
	r.maybeWarnWardOutdated(ctx)

	// Reclaim dead containers' writable layers before adding one more, so a busy
	// fleet can't exhaust the docker disk and wedge new launches (ward#272).
	r.sweepStaleContainers(ctx)
	if !c.Bool("no-pull") {
		if perr := r.Runner.Exec(ctx, "docker", "pull", plan.Image); perr != nil {
			fmt.Fprintf(os.Stderr, "%s: image pull failed (%v); trying the local image\n", label, perr)
		}
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, r.resolveAgentCreds(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()
	fmt.Fprintf(os.Stderr, "%s: opening an interactive %s session on %s in a fresh container...\n\n", label, mode.agentBinary(), repo.slug())
	return r.Runner.Exec(ctx, "docker", dockerCreateArgv(plan, envFile)...)
}

// printAgentSandboxPlan renders the resolved repo and the docker plan without
// cloning or firing - the dry-run preview for sandbox. There is no seed to show.
func printAgentSandboxPlan(c *cli.Command, p upPlan) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(p.Mode, "sandbox"))
	fmt.Fprintf(&b, "sandbox: agent runs interactive, attached, in a fresh ephemeral container (no seed)\n")
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
