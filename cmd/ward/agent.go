package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent.go wires the `ward agent <name> work <issue>` surface: sugar over
// `container up` that seeds a fresh container to work an issue. See docs/agent.md.

// agentIssueRef is a parsed issue reference for `ward agent ... work`. Only the
// owner/repo#N short form and the Forgejo issue URL are accepted (no GitHub).
type agentIssueRef struct {
	Owner  string
	Repo   string
	Number int
}

func (r agentIssueRef) String() string {
	return fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number)
}

// url renders the canonical Forgejo issue URL for the seeded prompt.
func (r agentIssueRef) url() string {
	return fmt.Sprintf("%s/%s/%s/issues/%d", strings.TrimRight(forgejoBaseURL, "/"), r.Owner, r.Repo, r.Number)
}

// agentIssueShortRE matches owner/repo#N.
var agentIssueShortRE = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)#(\d+)$`)

// agentIssueURLRE matches <forgejoBaseURL>/owner/repo/issues/N (trailing slash
// optional). A follow-up unifies this with cli-guard dispatch.parseIssueRef.
var agentIssueURLRE = regexp.MustCompile(`^` + regexp.QuoteMeta(strings.TrimRight(forgejoBaseURL, "/")) +
	`/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)/issues/(\d+)/?$`)

// parseAgentIssueRef resolves the work target from owner/repo#N or a Forgejo
// issue URL. The number is validated positive; everything else is a hard error.
func parseAgentIssueRef(s string) (agentIssueRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return agentIssueRef{}, fmt.Errorf("empty issue reference")
	}
	m := agentIssueShortRE.FindStringSubmatch(s)
	if m == nil {
		m = agentIssueURLRE.FindStringSubmatch(s)
	}
	if m == nil {
		return agentIssueRef{}, fmt.Errorf(
			"cannot parse issue ref %q: want owner/repo#N or %s/owner/repo/issues/N",
			s, strings.TrimRight(forgejoBaseURL, "/"))
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return agentIssueRef{}, fmt.Errorf("issue number must be a positive integer in %q", s)
	}
	return agentIssueRef{Owner: m[1], Repo: m[2], Number: n}, nil
}

// agentSeedPrompt is the lean instruction the in-container agent opens with:
// it names the issue and the first move (read it). See docs/agent.md.
func agentSeedPrompt(ref agentIssueRef, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "(untitled)"
	}
	return fmt.Sprintf(
		"Work on Forgejo issue %s (%q).\n\n"+
			"URL: %s\n\n"+
			"First action: read the full issue body and comment thread at that URL before "+
			"doing anything else. Then carry it end to end per your container doctrine - "+
			"implement, commit, merge to main, push - and close the issue with a commit "+
			"trailer: closes #%d.",
		ref, title, ref.url(), ref.Number)
}

// agentModes is the ordered set of agent subcommands ward exposes; each maps
// onto a containerMode (harness + context level). claude is the daily driver.
var agentModes = []containerMode{modeClaude, modeCodex, modeQwen, modeGoose}

// agentCommand is the `ward agent` umbrella: `ward agent <name> work <issue>`.
func agentCommand() *cli.Command {
	subs := make([]*cli.Command, 0, len(agentModes))
	for _, m := range agentModes {
		subs = append(subs, agentModeCommand(m))
	}
	return &cli.Command{
		Name:  "agent",
		Usage: "Send an agent into a fresh ephemeral container to carry a Forgejo issue end to end.",
		Description: `agent is the short verb over 'ward container': pick the harness by name
(claude|codex|qwen|goose), then 'work <issue>' resolves the issue's repo, spins up an
ephemeral least-access container, fresh-clones the repo inside it, and launches
the agent seeded to carry the issue to merge. One line replaces the full
'container up <repo> --mode <m> --branch <b>' stack plus a hand-written prompt.

  ward agent claude work coilyco-flight-deck/ward#98
  ward agent claude work https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/98
  ward agent codex work coilyco-flight-deck/ward#98 --print   # resolve + show the plan, run nothing

See docs/container.md for the container model (ephemeral, fresh-clone-inside,
reaper-backed). The agent runs under the container's bypassPermissions policy,
so 'work' is only accepted against a trusted owner.`,
		Commands: subs,
	}
}

// agentModeCommand builds `ward agent <mode>` with its work, headless, and task
// children.
func agentModeCommand(m containerMode) *cli.Command {
	return &cli.Command{
		Name:  string(m),
		Usage: fmt.Sprintf("Drive %s against a Forgejo issue in an ephemeral container.", m),
		Commands: []*cli.Command{
			agentSurfaceCommand(m, "work", false),
			agentSurfaceCommand(m, "headless", true),
			agentTaskCommand(m),
		},
	}
}

// agentSurfaceCommand builds `ward agent <mode> {work,headless} <issue>`: work
// is interactive, headless detaches + runs print mode. See docs/agent.md.
func agentSurfaceCommand(m containerMode, surface string, headless bool) *cli.Command {
	usage := "Resolve the issue's repo, spin up a fresh container, and seed the agent to carry it end to end."
	if headless {
		usage = "Like work, but detached + non-interactive (claude -p): fire-and-forget, read the container log."
	}
	flags := []cli.Flag{
		&cli.StringFlag{Name: "branch", Usage: "feature branch to create inside the clone (default: issue-<N>)"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault, Usage: "dev-base image to run"},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Usage: "image tag"},
		&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
		&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
		&cli.BoolFlag{Name: "print", Usage: "resolve the issue + seeded prompt + docker plan and exit; inject no push token, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	}
	if !headless {
		// headless always detaches, so only the interactive surface exposes --detach.
		flags = append(flags, &cli.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "run detached instead of interactive"})
	} else {
		// headless detaches into a fire-and-forget run; an interactive dispatch
		// gets a pre-flight feasibility check first (ward#137). --no-preflight is
		// the escape hatch for scripted runs launched from a TTY.
		flags = append(flags, &cli.BoolFlag{Name: "no-preflight", Usage: "skip the interactive pre-flight feasibility check before detaching"})
	}
	return &cli.Command{
		Name:      surface,
		Usage:     usage,
		ArgsUsage: "<owner/repo#N | forgejo-issue-url>",
		Flags:     flags,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(m) + "." + surface,
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentWork(ctx, cmd, m, surface, headless)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// resolvedWork bundles what resolveAgentWork produces: the parsed ref, the
// issue's title + body (body feeds the pre-flight read), and the seeded prompt.
type resolvedWork struct {
	Ref   agentIssueRef
	Title string
	Body  string
	Seed  string
}

// resolveAgentWork parses + trust-gates the ref, fetches the issue (failing fast
// before any container spins), and returns the ref, title, body, and seed prompt.
func (r *Runner) resolveAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string) (resolvedWork, error) {
	label := fmt.Sprintf("ward agent %s %s", mode, surface)
	ref, err := parseAgentIssueRef(c.Args().First())
	if err != nil {
		return resolvedWork{}, fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: the in-container agent runs under bypassPermissions, so only
	// spin one up for an owner in the primary-org set. Mirrors dispatch's check.
	if !r.ownerAllowed(ref.Owner) {
		return resolvedWork{}, fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, ref.Owner, strings.Join(r.primaryOrgs(), ", "))
	}
	issue, err := r.fetchForgejoIssue(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return resolvedWork{}, fmt.Errorf("%s: resolve issue %s: %w", label, ref, err)
	}
	if st := strings.ToLower(strings.TrimSpace(issue.State)); st != "" && st != "open" {
		fmt.Fprintf(os.Stderr, "%s: note: issue %s is %s, not open - working it anyway.\n", label, ref, st)
	}
	title := strings.TrimSpace(issue.Title)
	return resolvedWork{Ref: ref, Title: title, Body: issue.Body, Seed: agentSeedPrompt(ref, title)}, nil
}

// runAgentWork resolves the issue, composes the seed prompt, and launches the
// container plan. headless forces detach + print mode; --print runs no docker.
// An interactively-dispatched headless run first does a quick pre-flight check
// (ward#137): the agent reads the issue and the operator confirms before the
// fire-and-forget run detaches.
func (r *Runner) runAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string, headless bool) error {
	w, err := r.resolveAgentWork(ctx, c, mode, surface)
	if err != nil {
		return err
	}
	if headless && preflightWanted(c) {
		proceed, perr := r.runPreflight(ctx, mode, w)
		if perr != nil {
			return fmt.Errorf("ward agent %s %s: pre-flight: %w", mode, surface, perr)
		}
		if !proceed {
			fmt.Fprintf(os.Stderr, "ward agent %s %s: pre-flight declined; nothing launched.\n", mode, surface)
			return nil
		}
	}
	return r.launchAgentContainer(ctx, c, mode, surface, headless, w.Ref, w.Title, w.Seed)
}

// preflightTimeout caps the pre-flight read so a wedged agent can't hold the
// operator's terminal hostage before the real run even starts.
const preflightTimeout = 3 * time.Minute

// preflightWanted gates the headless pre-flight to the "dispatched interactively"
// case from ward#137: a human at the terminal, never a --print dry run, and
// honoring the --no-preflight escape hatch for scripted-from-a-TTY launches.
func preflightWanted(c *cli.Command) bool {
	return terminalAttached() && !c.Bool("print") && !c.Bool("no-preflight")
}

// preflightPrompt asks the about-to-detach agent for a quick feasibility read on
// the issue. ward never parses the verdict - it is shown to the operator, who
// makes the call at the confirm prompt - so this stays a pure, testable string.
func preflightPrompt(ref agentIssueRef, title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "(untitled)"
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "(no description provided)"
	}
	return fmt.Sprintf(
		"You are about to be sent, fire-and-forget, into an ephemeral container to carry "+
			"this Forgejo issue end to end on your own - implement, commit, merge to main, "+
			"push - with no human watching once you detach.\n\n"+
			"Before that detached run starts, give a quick PRE-FLIGHT read: based only on the "+
			"issue below, do you think you can carry it to merge unattended?\n\n"+
			"Issue: %s (%q)\n\n%s\n\n"+
			"Answer in 2-4 sentences naming the main risk or unknown, then a final line of "+
			"exactly \"GO\" if you would take it on unattended or \"NO-GO: <reason>\" if a human "+
			"should weigh in first. This is a judgment call, not a commitment - be honest about "+
			"ambiguity.",
		ref, title, body)
}

// runPreflight runs the about-to-detach agent's quick feasibility read, then asks
// the operator to confirm the detached run; it returns whether to proceed. A
// missing or non-claude agent binary skips the self-assessment but still confirms,
// so the "before detaching" gate holds even when the read itself can't run.
func (r *Runner) runPreflight(ctx context.Context, mode containerMode, w resolvedWork) (bool, error) {
	label := fmt.Sprintf("ward agent %s headless", mode)
	bin := mode.agentBinary()
	// Only claude has a settled host print-mode invocation today (and is the only
	// in-image agent); for other modes, skip the read and fall back to a plain
	// confirm rather than guess an invocation.
	if mode == modeClaude && hostHasBinary(bin) {
		fmt.Fprintf(os.Stderr, "%s: pre-flight - asking %s whether it can carry %s before detaching...\n\n", label, bin, w.Ref)
		pctx, cancel := context.WithTimeout(ctx, preflightTimeout)
		defer cancel()
		if err := r.Runner.Exec(pctx, bin, "-p", preflightPrompt(w.Ref, w.Title, w.Body)); err != nil {
			fmt.Fprintf(os.Stderr, "\n%s: pre-flight read did not complete (%v); decide from the issue itself.\n", label, err)
		}
		fmt.Fprintln(os.Stderr)
	} else {
		fmt.Fprintf(os.Stderr, "%s: %s self-assessment unavailable on this host; confirm from the issue itself.\n", label, bin)
	}
	return r.confirmProceed(fmt.Sprintf("Launch the detached headless run for %s? [y/N] ", w.Ref))
}

// hostHasBinary reports whether bin resolves on the host PATH.
func hostHasBinary(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// confirmProceed prints prompt and reads one line from the runner's stdin,
// returning true only on an explicit yes. Anything else - including EOF or a
// closed/piped stdin - is a no, so the detached run never fires unconfirmed.
func (r *Runner) confirmProceed(prompt string) (bool, error) {
	if r.Runner == nil || r.Runner.Stdin == nil {
		return false, nil
	}
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(r.Runner.Stdin).ReadString('\n')
	switch err {
	case nil, io.EOF:
		// A final line without a trailing newline still arrives alongside io.EOF.
	default:
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// launchAgentContainer turns a resolved (ref, title, seed) into the container
// plan and fires it - the shared tail of work, headless, and task. See docs/agent.md.
func (r *Runner) launchAgentContainer(ctx context.Context, c *cli.Command, mode containerMode, surface string, headless bool, ref agentIssueRef, title, seed string) error {
	label := fmt.Sprintf("ward agent %s %s", mode, surface)

	// headless always detaches; the interactive surface honors --detach.
	detached := headless || c.Bool("detach")
	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return err
	}
	// A detached run leaves its assets for the next sweep (it cannot delete the
	// still-mounted dir on return); an attached run cleans up on exit.
	if !detached {
		defer cleanupAssets()
	}

	cwd := resolveInvokeCWD()
	if cwd == "" {
		return fmt.Errorf("%s: cannot resolve the current directory", label)
	}
	repo := targetRepo{Owner: ref.Owner, Name: ref.Repo}
	plan := buildUpPlan(c, repo, mode, cwd, assetsDir, []string{seed})
	if plan.Branch == "" {
		plan.Branch = fmt.Sprintf("issue-%d", ref.Number)
	}
	// Override the generic ward-<repo>-<rand> name with one that names the issue
	// and harness, so a host running several agents can tell them apart.
	plan.Name = agentContainerName(repo, mode, ref.Number, randHex(4))
	plan.Headless = headless
	if detached {
		plan.Interactive = false
		plan.TTY = false
	}

	if c.Bool("print") {
		return printAgentPlan(c, plan, ref, title, seed, surface)
	}
	if !c.Bool("no-pull") {
		if perr := r.Runner.Exec(ctx, "docker", "pull", plan.Image); perr != nil {
			fmt.Fprintf(os.Stderr, "%s: image pull failed (%v); trying the local image\n", label, perr)
		}
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, r.claudeCredsForMode(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()
	return r.Runner.Exec(ctx, "docker", dockerCreateArgv(plan, envFile)...)
}

// agentTaskCommand builds `ward agent <mode> task [owner/repo]`: files an issue
// from --instructions, then runs the headless flow against it. See docs/agent.md.
func agentTaskCommand(m containerMode) *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "instructions", Aliases: []string{"i"}, Usage: "the task to file as the issue body (first line becomes the title)"},
		&cli.StringFlag{Name: "instructions-file", Usage: "read the instructions from a file instead of --instructions (escape hatch for long bodies)"},
		&cli.StringFlag{Name: "branch", Usage: "feature branch to create inside the clone (default: issue-<N>)"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault, Usage: "dev-base image to run"},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Usage: "image tag"},
		&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
		&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
		&cli.BoolFlag{Name: "print", Usage: "resolve the repo + the issue that would be filed + the docker plan and exit; file nothing, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	}
	return &cli.Command{
		Name:      "task",
		Usage:     "Like headless, but file the issue first: --instructions becomes a fresh Forgejo issue the agent then carries end to end and closes.",
		ArgsUsage: "[owner/repo]   (default: infer from the cwd's git origin)",
		Flags:     flags,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(m) + ".task",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentTask(ctx, cmd, m)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// taskInstructions reads the task body from --instructions, falling back to
// --instructions-file. Exactly one source must be non-empty.
func taskInstructions(c *cli.Command) (string, error) {
	inline := strings.TrimSpace(c.String("instructions"))
	file := strings.TrimSpace(c.String("instructions-file"))
	switch {
	case inline != "" && file != "":
		return "", fmt.Errorf("pass either --instructions or --instructions-file, not both")
	case inline != "":
		return inline, nil
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read --instructions-file %q: %w", file, err)
		}
		s := strings.TrimSpace(string(b))
		if s == "" {
			return "", fmt.Errorf("--instructions-file %q is empty", file)
		}
		return s, nil
	default:
		return "", fmt.Errorf("no task given: pass --instructions \"...\" or --instructions-file <path>")
	}
}

// taskTitleMaxLen caps the derived issue title so a wall-of-text first line
// doesn't become an unwieldy title.
const taskTitleMaxLen = 72

// taskTitle derives the issue title from the first non-empty line of the
// instructions, truncated on a rune boundary with an ellipsis.
func taskTitle(instructions string) string {
	first := ""
	for _, line := range strings.Split(instructions, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			first = s
			break
		}
	}
	if first == "" {
		first = "agent task"
	}
	r := []rune(first)
	if len(r) > taskTitleMaxLen {
		return strings.TrimSpace(string(r[:taskTitleMaxLen])) + "…"
	}
	return first
}

// taskBody is the filed issue body: the full instructions plus a provenance
// footer marking it as agent-filed rather than hand-written.
func taskBody(mode containerMode, instructions string) string {
	return fmt.Sprintf("%s\n\n---\nFiled by `ward agent %s task`.", instructions, mode)
}

// hostForgejoClient builds a write-capable Forgejo client from the SSM bearer
// token (the host path; the in-container reaper uses $FORGEJO_TOKEN instead).
func (r *Runner) hostForgejoClient(ctx context.Context) (*forgejoClient, error) {
	token, err := r.forgejoAPIToken(ctx)
	if err != nil {
		return nil, err
	}
	return newForgejoClient(forgejoBaseURL, token), nil
}

// runAgentTask resolves the repo, files an issue from --instructions, and runs
// the headless container seeded to carry + close it. --print files nothing.
func (r *Runner) runAgentTask(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := fmt.Sprintf("ward agent %s task", mode)
	repo, _, err := r.resolveTarget(ctx, c.Args().First())
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Same trust gate as work/headless: the container runs bypassPermissions, so
	// only file + work against an owner in the primary-org set.
	if !r.ownerAllowed(repo.Owner) {
		return fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, repo.Owner, strings.Join(r.primaryOrgs(), ", "))
	}
	instructions, err := taskInstructions(c)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	title := taskTitle(instructions)
	body := taskBody(mode, instructions)

	if c.Bool("print") {
		return printAgentTaskPlan(c, mode, repo, title, body)
	}

	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	number, err := cl.createIssue(ctx, repo.Owner, repo.Name, title, body)
	if err != nil {
		return fmt.Errorf("%s: file issue in %s/%s: %w", label, repo.Owner, repo.Name, err)
	}
	ref := agentIssueRef{Owner: repo.Owner, Repo: repo.Name, Number: number}
	fmt.Fprintf(os.Stderr, "%s: filed %s - %s\n", label, ref, ref.url())

	seed := agentSeedPrompt(ref, title)
	return r.launchAgentContainer(ctx, c, mode, "task", true, ref, title, seed)
}

// printAgentTaskPlan renders the repo, the issue that *would* be filed, and the
// docker plan without filing or firing - the dry-run preview for task.
func printAgentTaskPlan(c *cli.Command, mode containerMode, repo targetRepo, title, body string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	// A placeholder ref renders the seed shape; the real number is only known
	// once the issue is filed (which --print deliberately skips).
	previewRef := agentIssueRef{Owner: repo.Owner, Repo: repo.Name, Number: 0}
	seed := agentSeedPrompt(previewRef, title)
	plan := buildUpPlan(c, repo, mode, "", "", []string{seed})
	plan.Headless = true
	plan.Interactive = false
	plan.TTY = false
	if plan.Branch == "" {
		plan.Branch = "issue-<N>"
	}
	// Mirror the live name shape; the real issue number lands once filed, so the
	// placeholder reads issue-<N> like the branch above.
	plan.Name = fmt.Sprintf("%s-%s-issue-<N>-%s-<rand>", containerNamePrefix, safeRepoName(repo), mode)

	var b strings.Builder
	fmt.Fprintf(&b, "# ward agent %s task (print)\n", mode)
	fmt.Fprintf(&b, "headless: agent runs detached in print mode (-p)\n")
	fmt.Fprintf(&b, "repo:    %s\n", repo.slug())
	fmt.Fprintf(&b, "branch:  %s\n", plan.Branch)
	fmt.Fprintf(&b, "name:    %s\n", plan.Name)
	fmt.Fprintf(&b, "----- issue to file -----\ntitle: %s\n\n%s\n----- end -----\n", title, body)
	fmt.Fprintf(&b, "----- seeded prompt (#N filled once filed) -----\n%s\n----- end -----\n", seed)
	if c.Bool("no-pull") {
		fmt.Fprintf(&b, "# pull skipped (--no-pull); image: %s\n", plan.Image)
	} else {
		fmt.Fprintf(&b, "docker pull %s\n", plan.Image)
	}
	fmt.Fprintf(&b, "docker %s\n", strings.Join(dockerCreateArgv(plan, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, b.String())
	return err
}

// ownerAllowed reports whether owner is in ward's primary-org trust set.
func (r *Runner) ownerAllowed(owner string) bool {
	for _, o := range r.primaryOrgs() {
		if owner == o {
			return true
		}
	}
	return false
}

// printAgentPlan renders the resolved issue, the seeded prompt, and the docker
// plan without firing - the dry-run preview, safe with no docker daemon.
func printAgentPlan(c *cli.Command, p upPlan, ref agentIssueRef, title, seed, surface string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# ward agent %s %s (print)\n", p.Mode, surface)
	if p.Headless {
		fmt.Fprintf(&b, "headless: agent runs detached in print mode (-p)\n")
	}
	fmt.Fprintf(&b, "issue:   %s\n", ref)
	fmt.Fprintf(&b, "url:     %s\n", ref.url())
	fmt.Fprintf(&b, "title:   %s\n", title)
	fmt.Fprintf(&b, "repo:    %s\n", p.Repo.slug())
	fmt.Fprintf(&b, "branch:  %s\n", p.Branch)
	fmt.Fprintf(&b, "name:    %s\n", p.Name)
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
