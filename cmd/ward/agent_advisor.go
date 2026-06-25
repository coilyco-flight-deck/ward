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

// agent_advisor.go wires `ward agent advisor`, the counsel role (ward#347, merging
// reply + ask by arg type; reply lives in agent_reply.go). See docs/agent-advisor.md.

// agentAdvisorFlags is the advisor role's flag set: the reply depth ladder (ref mode)
// unioned with the scratch-container flags the inline answer (freeform mode) needs.
func agentAdvisorFlags() []cli.Flag {
	return []cli.Flag{
		agentDriverFlag(),
		// Ref mode (was `reply`): how hard the host one-shot research digs.
		&cli.StringFlag{
			Name:    "thoroughness",
			Aliases: []string{"depth"},
			Value:   defaultReplyThoroughness,
			Usage:   "ref mode: how hard to dig: quick|standard|deep (deeper gets a longer timeout)",
		},
		// Freeform mode (was `ask`): the fresh container the inline answer leans on.
		&cli.StringFlag{Name: "repo", Usage: "freeform mode: owner/repo to clone for context (default: inferred from the cwd's git origin)"},
		&cli.StringSliceFlag{Name: "with-repo", Usage: "freeform mode: clone an additional repo for context (owner/name; repeatable), landed under /workspace alongside the primary repo (ward#230)."},
		&cli.StringFlag{Name: "image", Value: containerImageDefault, Sources: cli.EnvVars(envAgentImage), Usage: "dev-base image to run (env: WARD_AGENT_IMAGE)"},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Sources: cli.EnvVars(envAgentTag), Usage: "image tag (env: WARD_AGENT_TAG)"},
		&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
		&cli.StringFlag{Name: "ward-version", Sources: cli.EnvVars(envAgentVersion), Usage: "ward release the container downloads (default: this host's ward; env: WARD_AGENT_VERSION)"},
		&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
		hostNetFlag(),
		tsSidecarFlag(),
		&cli.BoolFlag{Name: "print", Usage: "resolve the inputs + render the prompt + plan and exit; research nothing, post nothing, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	}
}

// agentAdvisorCommand builds `ward agent advisor`: a ref researches the issue and posts
// a comment (was reply), freeform text answers inline (was ask). docs/agent-advisor.md.
func agentAdvisorCommand() *cli.Command {
	return &cli.Command{
		Name: "advisor",
		Usage: "Answer without writing code: a ref researches the issue and posts the answer as a comment; " +
			"freeform text answers the question inline. No code change.",
		ArgsUsage: "<owner/repo#N | forgejo-issue-url> <prompt> | '<question>'",
		Flags:     agentAdvisorFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentDriver(c)
			if err != nil {
				return fmt.Errorf("ward agent advisor: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".advisor",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentAdvisor(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentAdvisor dispatches by argument type (ward#347): a parseable ref researches
// the issue and posts a comment (was reply); anything else answers inline (was ask).
func (r *Runner) runAgentAdvisor(ctx context.Context, c *cli.Command, mode containerMode) error {
	arg := strings.TrimSpace(c.Args().First())
	if _, err := parseAgentIssueRef(arg); err == nil {
		return r.runAgentReply(ctx, c, mode)
	}
	return r.runAgentAsk(ctx, c, mode)
}

// runAgentAsk seeds the question, spins up a fresh attached container, and runs the
// agent one-shot so the answer streams inline (advisor freeform mode; ward#347, was ask).
func (r *Runner) runAgentAsk(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "advisor")

	// The whole arg tail is the question, joined so an unquoted multi-word
	// question still works (the canonical form is one quoted arg).
	question := strings.TrimSpace(strings.Join(c.Args().Slice(), " "))
	if question == "" {
		return fmt.Errorf("%s: no question: pass it as the argument, e.g. %s \"how does X work here?\"", label, label)
	}

	// The context repo is --repo, else inferred from the cwd's git origin (the
	// same target resolution the container bring-up uses).
	repo, cwd, err := r.resolveTarget(ctx, strings.TrimSpace(c.String("repo")))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: this spins a bypassPermissions container and clones the repo, so
	// only act on an owner in the primary-org set - the same gate the other roles apply.
	if !r.ownerAllowed(repo.Owner) {
		return fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, repo.Owner, strings.Join(r.primaryOrgs(), ", "))
	}

	seed := askPrompt(question)

	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return err
	}
	// A freeform answer runs attached and ephemeral, so its assets clean up on return.
	defer cleanupAssets()

	plan, err := buildUpPlan(c, repo, mode, cwd, assetsDir, []string{seed})
	if err != nil {
		return err
	}
	plan.Ask = true
	// Name it ward-<repo>-ask-<mode>-<rand> so `docker ps` tells an inline answer apart
	// from a carry run or a bare interactive bring-up.
	plan.Name = fmt.Sprintf("%s-%s-ask-%s-%s", containerNamePrefix, safeRepoName(repo), mode, randHex())

	if c.Bool("print") {
		return printAgentAskPlan(c, plan, question, seed)
	}

	// A freeform answer is interactive (you watch it), so this dispatch is the moment to
	// surface a stale-ward reminder before the container spins (ward#143).
	r.maybeWarnWardOutdated(ctx)

	// Reclaim dead containers' writable layers before adding one more, so a busy
	// fleet can't exhaust the docker disk and wedge new launches (ward#272).
	r.sweepStaleContainers(ctx)
	if !c.Bool("no-pull") {
		r.pullAgentImage(ctx, plan, label)
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), r.resolveAgentCreds(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()
	fmt.Fprintf(os.Stderr, "%s: answering with %s about %s in a fresh container...\n\n", label, mode.agentBinary(), repo.slug())
	return r.createAgentContainer(ctx, plan, envFile)
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

// printAgentAskPlan renders the repo, question, seeded prompt, and docker plan without
// cloning or firing - the dry-run preview for the advisor's freeform mode.
func printAgentAskPlan(c *cli.Command, p upPlan, question, seed string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print, freeform mode)\n", agentCmdline(p.Mode, "advisor"))
	fmt.Fprintf(&b, "advisor: agent runs one-shot, attached, in a fresh ephemeral container\n")
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
