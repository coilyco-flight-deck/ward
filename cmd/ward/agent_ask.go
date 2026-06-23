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

// agent_ask.go wires `ward agent <name> ask <question>`: run the agent one-shot in a
// fresh container, streaming the answer inline. See docs/agent-ask.md.

// agentAskCommand builds `ward agent <mode> ask <question>` so the answer can lean
// on the container's fresh clone + operating context. See docs/agent-ask.md.
func agentAskCommand(m containerMode) *cli.Command {
	return &cli.Command{
		Name: "ask",
		Usage: "Ask the agent a one-shot question inside a fresh container (repo clone + operating context) " +
			"and stream the answer inline - no issue, no code change, no comment.",
		ArgsUsage: "<question>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "repo", Usage: "owner/repo to clone for context (default: inferred from the cwd's git origin)"},
			&cli.StringSliceFlag{Name: "with-repo", Usage: "clone an additional repo for context (owner/name; repeatable), landed under /workspace alongside the primary repo (ward#230)."},
			&cli.StringFlag{Name: "image", Value: containerImageDefault, Usage: "dev-base image to run"},
			&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Usage: "image tag"},
			&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
			&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
			&cli.BoolFlag{Name: "print", Usage: "resolve the repo + question + docker plan and exit; clone nothing, run nothing"},
			&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(m) + ".ask",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentAsk(ctx, cmd, m)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentAsk resolves the context repo, seeds the question, spins up a fresh
// attached container, and runs the agent one-shot so the answer streams inline.
func (r *Runner) runAgentAsk(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := fmt.Sprintf("ward agent %s ask", mode)

	// The whole arg tail is the question, joined so an unquoted multi-word
	// question still works (the canonical form is one quoted arg).
	question := strings.TrimSpace(strings.Join(c.Args().Slice(), " "))
	if question == "" {
		return fmt.Errorf("%s: no question: pass it as the argument, e.g. %s \"how does X work here?\"", label, label)
	}

	// The context repo is --repo, else inferred from the cwd's git origin (the
	// same resolution `ward container up` uses for its positional).
	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: ask spins a bypassPermissions container and clones the repo, so
	// only act on an owner in the primary-org set - the same gate work/task apply.
	if !r.ownerAllowed(repo.Owner) {
		return fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, repo.Owner, strings.Join(r.primaryOrgs(), ", "))
	}

	seed := askPrompt(question)

	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return err
	}
	// ask always runs attached and ephemeral, so its assets clean up on return.
	defer cleanupAssets()

	plan, err := buildUpPlan(c, repo, mode, cwd, assetsDir, []string{seed})
	if err != nil {
		return err
	}
	plan.Ask = true
	// Name it ward-<repo>-ask-<mode>-<rand> so `docker ps` tells an ask run apart
	// from a carry run or a bare `container up`.
	plan.Name = fmt.Sprintf("%s-%s-ask-%s-%s", containerNamePrefix, safeRepoName(repo), mode, randHex(4))

	if c.Bool("print") {
		return printAgentAskPlan(c, plan, question, seed)
	}

	// ask is interactive (you watch the answer), so this dispatch is the moment to
	// surface a stale-ward reminder before the container spins (ward#143).
	r.maybeWarnWardOutdated(ctx)

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
	fmt.Fprintf(os.Stderr, "%s: asking %s about %s in a fresh container...\n\n", label, mode.agentBinary(), repo.slug())
	return r.Runner.Exec(ctx, "docker", dockerCreateArgv(plan, envFile)...)
}

// askPrompt light-wraps the question so the in-container agent answers inline (no
// preamble, no sign-off) and stays read-only rather than carrying work. Pure.
func askPrompt(question string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		question = "(no question given)"
	}
	return fmt.Sprintf(
		"Answer the question below directly and concisely. Your output streams straight to a "+
			"terminal for a human to read inline, so write the answer itself in clean text or "+
			"GitHub-flavored markdown - no preamble like \"here is my answer\", no sign-off.\n\n"+
			"You are NOT implementing anything, NOT changing code, and NOT carrying any issue to merge - "+
			"this is a one-shot question. You have a fresh clone of this repo and the usual operating "+
			"context to draw on: read the code, run read-only commands, and search as needed to ground "+
			"the answer, but do not commit, push, or open anything.\n\n"+
			"----- the question -----\n%s\n----- end question -----",
		question)
}

// printAgentAskPlan renders the resolved repo, the question, the seeded prompt,
// and the docker plan without cloning or firing - the dry-run preview for ask.
func printAgentAskPlan(c *cli.Command, p upPlan, question, seed string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# ward agent %s ask (print)\n", p.Mode)
	fmt.Fprintf(&b, "ask: agent runs one-shot, attached, in a fresh ephemeral container\n")
	fmt.Fprintf(&b, "repo:   %s\n", p.Repo.slug())
	fmt.Fprintf(&b, "name:   %s\n", p.Name)
	fmt.Fprintf(&b, "----- question -----\n%s\n----- end -----\n", question)
	fmt.Fprintf(&b, "----- seeded prompt -----\n%s\n----- end -----\n", seed)
	if c.Bool("no-pull") {
		fmt.Fprintf(&b, "# pull skipped (--no-pull); image: %s\n", p.Image)
	} else {
		fmt.Fprintf(&b, "docker pull %s\n", p.Image)
	}
	fmt.Fprintf(&b, "docker %s\n", strings.Join(dockerCreateArgv(p, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, b.String())
	return err
}
