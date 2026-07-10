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
	flags := agentHarnessFlags()
	flags = append(flags,
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
		&cli.StringFlag{Name: "instructions-file", Usage: "freeform mode: read the question body from a file (escape hatch for long bodies, or a bare owner/repo + a brief)"},
		// Freeform mode is interactive by default under a TTY (ward#388); --oneshot
		// forces the streamed one-shot answer even on a terminal (scripting escape hatch).
		&cli.BoolFlag{Name: "oneshot", Aliases: []string{"answer"}, Usage: "freeform mode: force the one-shot streamed answer even under a TTY (default: interactive seeded session when a terminal is attached)"},
		configFlag(),
	)
	flags = append(flags, agentImageFlags()...)
	return append(flags,
		&cli.BoolFlag{Name: "no-tailnet", Usage: "opt out of advisor's default live-observe tailnet route and stay isolated"},
		&cli.BoolFlag{Name: "print", Usage: "resolve the inputs + render the prompt + plan and exit; research nothing, post nothing, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	)
}

// agentAdvisorCommand builds `ward agent advisor`: a ref researches the issue and posts
// a comment (was reply), freeform text answers inline (was ask). docs/agent-advisor.md.
func agentAdvisorCommand() *cli.Command {
	return &cli.Command{
		Name: "advisor",
		Usage: "Answer without writing code: a ref researches the issue and posts the answer as a comment; " +
			"freeform text opens an interactive seeded session (one-shot streamed answer with no TTY or --oneshot). The advisor role holds the live-observe guardfile set (tailnet + ~/.aws) by default; use --no-tailnet to stay isolated. No code change.",
		ArgsUsage: "<owner/repo#N | issue-url> [prompt] | '<question>'",
		Flags:     agentAdvisorFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := surfaceDispatchMode(c)
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
		if forwarded, ferr := r.maybeForwardAgentDispatchToHostBroker(ctx, c, "advisor", mode); forwarded {
			return ferr
		}
		return r.runAgentReply(ctx, c, mode)
	}
	return r.runAgentAsk(ctx, c, mode)
}

// runAgentAsk seeds the freeform question and spins a fresh attached container (was ask):
// interactive by default, one-shot with no TTY or --oneshot (ward#347, ward#388).
func (r *Runner) runAgentAsk(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "advisor")

	repoArg, err := advisorFreeformRepoArg(c, label)
	if err != nil {
		return err
	}
	question, err := advisorFreeformQuestion(c, label)
	if err != nil {
		return err
	}

	return r.runAgentAdvisorFreeform(ctx, c, mode, label, repoArg, question)
}

func (r *Runner) runAgentAdvisorFreeform(ctx context.Context, c *cli.Command, mode containerMode, label, repoArg, question string) error {
	repo, plan, seed, cleanupAssets, err := r.advisorFreeformPlan(ctx, c, mode, label, repoArg, question)
	if err != nil {
		return err
	}
	defer cleanupAssets()

	// The context repo is --repo, or an explicit owner/repo paired with
	// --instructions-file, else inferred from the cwd git origin.
	if c.Bool("print") {
		return printAgentAskPlan(c, plan, question, seed)
	}

	// A freeform answer is interactive (you watch it), so this dispatch is the moment to
	// surface a stale-ward reminder before the container spins (ward#143).
	r.maybeWarnWardOutdated(ctx)

	// Ready the tailnet network, sweep dead containers, then pull - the shared
	// pre-launch steps; a missing ward-tailnet network is created here (ward#597, #272).
	if err := r.prelaunchDispatch(ctx, c, plan, label); err != nil {
		return err
	}
	launchCreds := r.resolveLaunchCreds(ctx, &plan, mode)
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), plan.Forge, launchCreds)
	if err != nil {
		return err
	}
	defer cleanupEnv()
	if plan.Ask {
		fmt.Fprintf(os.Stderr, "%s: answering with %s about %s in a fresh container...\n\n", label, lookupAgent(mode).Record().Binary, repo.slug())
	} else {
		fmt.Fprintf(os.Stderr, "%s: opening an interactive %s session about %s in a fresh container (read-only; follow up as you like)...\n\n", label, lookupAgent(mode).Record().Binary, repo.slug())
	}
	return r.createAgentContainer(ctx, plan, envFile)
}

func (r *Runner) advisorFreeformPlan(ctx context.Context, c *cli.Command, mode containerMode, label, repoArg, question string) (targetRepo, upPlan, string, func(), error) {
	repo, cwd, err := r.resolveTarget(ctx, repoArg)
	if err != nil {
		return targetRepo{}, upPlan{}, "", func() {}, fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: this spins a bypassPermissions container and clones the repo, so
	// only act on an owner in the trusted-owner set - the same gate the other roles apply.
	if !r.ownerAllowed(repo.Owner) {
		return targetRepo{}, upPlan{}, "", func() {}, r.untrustedOwnerErr(label, repo.Owner)
	}

	// Interactive by default under a TTY; one-shot streamed answer with no TTY
	// (piped/CI/host-broker) or --oneshot. terminalAttached() drives plan.TTY (ward#388).
	oneshot := c.Bool("oneshot") || !terminalAttached()
	seed := interactivePrompt(question)
	if oneshot {
		seed = askPrompt(question)
	}

	assetsDir, cleanupAssets, err := writeContainerAssets(ctx, r, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
	if err != nil {
		return targetRepo{}, upPlan{}, "", func() {}, err
	}
	plan, err := buildUpPlan(c, repo, mode, roleAdvisor, cwd, assetsDir, []string{seed}, false)
	if err != nil {
		cleanupAssets()
		return targetRepo{}, upPlan{}, "", func() {}, err
	}
	plan.ReadOnly = true
	// Only the one-shot path exports WARD_ASK=1 (claude -p): the interactive default
	// leaves it unset so the entrypoint takes the plain seeded `claude <seed>` branch.
	plan.Ask = oneshot
	// Name it advisor-<driver>-<machine> (issueless, so the machine id disambiguates
	// concurrent answers) and label it ward.role=advisor (ward#364).
	plan.Role = roleAdvisor
	plan.Name = containerRoleName(roleAdvisor, mode, repo, 0, plan.Machine)
	return repo, plan, seed, cleanupAssets, nil
}

func advisorFreeformRepoArg(c *cli.Command, label string) (string, error) {
	repoArg := strings.TrimSpace(c.String("repo"))
	if strings.TrimSpace(c.String("instructions-file")) == "" {
		return repoArg, nil
	}
	arg := strings.TrimSpace(c.Args().First())
	if arg == "" {
		return repoArg, nil
	}
	repo, err := parseRepoRef(arg)
	if err != nil {
		return "", fmt.Errorf("%s: got a freeform question as the positional argument and also --instructions-file; pass the repo one way, not both", label)
	}
	return repo.slug(), nil
}

func advisorFreeformQuestion(c *cli.Command, label string) (string, error) {
	if strings.TrimSpace(c.String("instructions-file")) != "" {
		question, err := taskInstructions(c)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		return question, nil
	}
	// The whole arg tail is the question, joined so an unquoted multi-word
	// question still works (the canonical form is one quoted arg).
	question := strings.TrimSpace(strings.Join(c.Args().Slice(), " "))
	if question == "" {
		return "", fmt.Errorf("%s: no question: pass it as the argument, e.g. %s \"how does X work here?\"", label, label)
	}
	return question, nil
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

// interactivePrompt frames the freeform advisor's default seeded session (ward#388): a
// conversational opener inviting follow-up, same read-only guardrails as askPrompt. Pure.
func interactivePrompt(question string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		question = "(no question given)"
	}
	return fmt.Sprintf(
		"You are in an interactive advisory session with a human at the terminal. Answer the "+
			"question below, then stay and take follow-ups conversationally - this is a live "+
			"back-and-forth, not a one-shot, so it is fine to ask a clarifying question and to "+
			"build on earlier answers.\n\n"+
			"You are an advisor, NOT an implementer: do NOT change code, commit, push, open "+
			"anything, or carry any issue to merge. You have a fresh clone of this repo and the "+
			"usual operating context - read the code, run read-only commands, and search as needed "+
			"to ground your answers, but every action stays read-only.\n\n"+
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
	if p.Ask {
		fmt.Fprintf(&b, "advisor: agent runs one-shot, attached, in a fresh ephemeral container (WARD_ASK=1; --oneshot or no TTY)\n")
	} else {
		fmt.Fprintf(&b, "advisor: agent runs interactive + seeded, attached, in a fresh ephemeral container (TTY attached; follow-ups welcome)\n")
	}
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
