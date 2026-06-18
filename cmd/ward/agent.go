package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

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
var agentModes = []containerMode{modeClaude, modeCodex, modeQwen}

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
(claude|codex|qwen), then 'work <issue>' resolves the issue's repo, spins up an
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
		&cli.BoolFlag{Name: "force", Usage: "skip the local + remote concurrency reservation checks (reclaim a stale or foreign hold)"},
	}
	if !headless {
		// headless always detaches, so only the interactive surface exposes --detach.
		flags = append(flags, &cli.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "run detached instead of interactive"})
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

// resolveAgentWork parses + trust-gates the ref, fetches the issue (failing fast
// before any container spins), and returns the ref, title, and seeded prompt.
func (r *Runner) resolveAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string) (agentIssueRef, string, string, error) {
	label := fmt.Sprintf("ward agent %s %s", mode, surface)
	ref, err := parseAgentIssueRef(c.Args().First())
	if err != nil {
		return agentIssueRef{}, "", "", fmt.Errorf("%s: %w", label, err)
	}
	// Trust gate: the in-container agent runs under bypassPermissions, so only
	// spin one up for an owner in the primary-org set. Mirrors dispatch's check.
	if !r.ownerAllowed(ref.Owner) {
		return agentIssueRef{}, "", "", fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, ref.Owner, strings.Join(r.primaryOrgs(), ", "))
	}
	issue, err := r.fetchForgejoIssue(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return agentIssueRef{}, "", "", fmt.Errorf("%s: resolve issue %s: %w", label, ref, err)
	}
	if st := strings.ToLower(strings.TrimSpace(issue.State)); st != "" && st != "open" {
		fmt.Fprintf(os.Stderr, "%s: note: issue %s is %s, not open - working it anyway.\n", label, ref, st)
	}
	return ref, strings.TrimSpace(issue.Title), agentSeedPrompt(ref, issue.Title), nil
}

// runAgentWork resolves the issue, composes the seed prompt, and launches the
// container plan. headless forces detach + print mode; --print runs no docker.
func (r *Runner) runAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string, headless bool) error {
	ref, title, seed, err := r.resolveAgentWork(ctx, c, mode, surface)
	if err != nil {
		return err
	}
	return r.launchAgentContainer(ctx, c, mode, surface, headless, ref, title, seed)
}

// buildAgentPlan composes the container plan for a resolved issue: the seeded
// argv, the issue-<N> branch, and the issue-naming container name. detached
// strips the interactive/TTY flags so a backgrounded run never grabs a pty.
func buildAgentPlan(c *cli.Command, mode containerMode, ref agentIssueRef, seed string, headless, detached bool, assetsDir string) (upPlan, error) {
	cwd := resolveInvokeCWD()
	if cwd == "" {
		return upPlan{}, fmt.Errorf("cannot resolve the current directory")
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
	return plan, nil
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

	plan, err := buildAgentPlan(c, mode, ref, seed, headless, detached, assetsDir)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	if c.Bool("print") {
		return printAgentPlan(c, plan, ref, title, seed, surface)
	}

	// Reserve the issue so a second run - on this host (the file sentinel) or
	// another (the Forgejo marker comment) - won't redundantly work it. A
	// detached run outlives this process, so its local sentinel must persist for
	// the container's lifetime (TTL- and liveness-bounded); an attached run
	// releases the hold when it returns.
	releaseReservation, err := r.reserveIssue(ctx, label, mode, ref, plan.Name, plan.Branch, c.Bool("force"))
	if err != nil {
		return err
	}
	if !detached {
		defer releaseReservation()
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
		&cli.BoolFlag{Name: "force", Usage: "skip the local + remote concurrency reservation checks (reclaim a stale or foreign hold)"},
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
