package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/issueref"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/ownertrust"
	"github.com/urfave/cli/v3"
)

// agent.go wires the `ward agent` umbrella + the shared carry internals the engineer
// role uses (ward#263, ward#347), sharing the bring-up Go directly. See docs/agent.md.

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

// repoSlug renders the owner/repo pair without the issue number.
func (r agentIssueRef) repoSlug() string {
	return r.Owner + "/" + r.Repo
}

// url renders the canonical Forgejo issue URL for the seeded prompt.
func (r agentIssueRef) url() string {
	return fmt.Sprintf("%s/%s/%s/issues/%d", strings.TrimRight(forgejoBaseURL, "/"), r.Owner, r.Repo, r.Number)
}

// parseAgentIssueRef resolves owner/repo#N, a Forgejo URL, or a bare #N / N via
// cli-guard's pkg/issueref; ward keeps the task-verb steer (ward#234, ward#282).
func parseAgentIssueRef(s string) (agentIssueRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return agentIssueRef{}, fmt.Errorf("empty issue reference")
	}
	ref, err := issueref.Parse(s, forgejoBaseURL)
	if err == nil {
		return agentIssueRef{Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number}, nil
	}
	// A non-issue URL is a valid freeform pointer, just not an issue ref -
	// steer to the task verb that carries arbitrary pointers (ward#234).
	if strings.Contains(s, "://") {
		return agentIssueRef{}, fmt.Errorf(
			"cannot parse issue ref %q: want owner/repo#N, a bare #N, or %s/owner/repo/issues/N; "+
				"for a non-issue pointer (a CI run, job, or commit URL), hand it to the engineer "+
				"role's freeform mode instead: ward agent engineer '<url>'",
			s, strings.TrimRight(forgejoBaseURL, "/"))
	}
	return agentIssueRef{}, err
}

// resolveAgentIssueRef parses the ref and, for a bare #N / N, fills owner/repo from
// the cwd's git origin via resolveTarget - the inference ask/task use (ward#282).
func (r *Runner) resolveAgentIssueRef(ctx context.Context, arg string) (agentIssueRef, error) {
	ref, err := parseAgentIssueRef(arg)
	if err != nil {
		return agentIssueRef{}, err
	}
	if ref.Owner != "" && ref.Repo != "" {
		return ref, nil
	}
	repo, _, terr := r.resolveTarget(ctx, "")
	if terr != nil {
		return agentIssueRef{}, fmt.Errorf(
			"bare issue ref %q needs a repo, but the current directory has no git origin to infer one from "+
				"(use owner/repo#%d or run from inside the repo's checkout): %w", arg, ref.Number, terr)
	}
	ref.Owner, ref.Repo = repo.Owner, repo.Name
	return ref, nil
}

// markdownImageRE matches inline ![alt](url) image embeds.
var markdownImageRE = regexp.MustCompile(`!\[[^\]]*\]\([^)\s]*\)`)

// bareImageURLRE matches a standalone http(s) URL ending in a common image
// extension (with an optional query string), e.g. a pasted screenshot link.
var bareImageURLRE = regexp.MustCompile(`(?i)https?://\S+?\.(?:png|jpe?g|gif|webp|bmp|svg|tiff?)(?:\?\S*)?`)

// collapseBlankRunsRE squashes a run of 3+ newlines (left behind once media is
// stripped) back down to a single blank line.
var collapseBlankRunsRE = regexp.MustCompile(`\n{3,}`)

// stripIssueMedia drops ![..](..) embeds and bare image URLs (then tidies the
// gaps), so a non-vision harness has no screenshot to read_image (ward#157).
func stripIssueMedia(body string) string {
	body = markdownImageRE.ReplaceAllString(body, "")
	body = bareImageURLRE.ReplaceAllString(body, "")
	body = collapseBlankRunsRE.ReplaceAllString(body, "\n\n")
	return strings.TrimSpace(body)
}

// emptyBodySeedAction is the first move when an issue has no body: work from the
// title, don't go hunting content that isn't there (ward#157). See docs/agent.md.
const emptyBodySeedAction = "This issue has no body, so work from the title alone - do not hunt for " +
	"issue content, screenshots, or other artifacts that are not there (an empty body is not an " +
	"invitation to invent one). The comment thread at that URL may hold later context worth a quick read."

// headlessReflectionAction is the headless run's closing move (ward#281, ward#310):
// a "how it felt" retro led by a WARD-OUTCOME line. See docs/agent-director.md.
const headlessReflectionAction = "Finally, as your very last step - only after the work is committed, merged " +
	"to main, and pushed - post a SHORT comment on this issue (a few sentences, in your own voice) on how the " +
	"implementation \"felt\": how the work went, anything that surprised you or fought back, how confident you " +
	"are in the result, and any rough edges or follow-ups worth filing. This is a candid retrospective, not a " +
	"status report or a changelog - keep it brief and honest, and post it even if the work went smoothly.\n\n" +
	"Begin that final comment with a single machine-readable status line - its very first line, exactly one of:\n" +
	"  `" + wardOutcomeMarker + " done - <one line on what landed>`\n" +
	"  `" + wardOutcomeMarker + " blocked - <the one specific decision or piece of information you need from a human>`\n" +
	"  `" + wardOutcomeMarker + " failed - <why, briefly>`\n" +
	"then your retrospective on the lines below it. A supervising director loop (ward agent director) reads only that " +
	"first line to classify the run, so for a normal carry that you merged and pushed it is `" + wardOutcomeMarker +
	" done`; reserve blocked/failed for a run that genuinely could not land."

// grantedRepoDoneClause widens the done-condition for a --repo grant (ward#291):
// every granted repo must be pushed AND verified landed, not just the issue's repo.
func grantedRepoDoneClause(extra []targetRepo) string {
	if len(extra) == 0 {
		return ""
	}
	slugs := make([]string, len(extra))
	for i, repo := range extra {
		slugs[i] = repo.slug()
	}
	joined := strings.Join(slugs, ", ")
	return fmt.Sprintf(
		"\n\nThis run was GRANTED EXTRA WRITABLE REPOS via --repo: %s (full feature copies "+
			"under /workspace beside the issue's repo). Your done-condition is NOT just the "+
			"issue's own repo: every granted repo you touch must be pushed AND VERIFIED to have "+
			"landed. After pushing each one, fetch its remote and confirm your push actually "+
			"advanced the target ref - local HEAD must match the freshly-fetched remote main - "+
			"because a secondary push can be silently rejected (a non-fast-forward on a busy main, "+
			"a dead/rotated PAT) while the primary push succeeds. Do NOT post the closing "+
			"retrospective or treat the issue as done until every granted repo is verified landed. "+
			"A granted repo that did not land is a hard failure to call out, not a silent success. "+
			"And when a --repo grant exists only to push work into that second repo, prefer filing "+
			"that work as its own native issue in that repo instead - a single-repo run sidesteps "+
			"this cross-repo push failure mode entirely.",
		joined)
}

// agentSeedPrompt seeds the agent: the issue, a first move (ward#157), --details
// (ward#167), a granted-repo done-condition (ward#291), a reflection (ward#281).
func agentSeedPrompt(ref agentIssueRef, title, body, details string, mode containerMode, headless bool, extra []targetRepo) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "(untitled)"
	}
	body = strings.TrimSpace(body)

	action := "First action: read the full issue body and comment thread at that URL before doing anything else."
	inline := ""
	switch {
	case body == "":
		action = emptyBodySeedAction
	case !mode.visionCapable():
		if stripped := stripIssueMedia(body); stripped == "" {
			// The body was nothing but media; once stripped it's effectively empty.
			action = emptyBodySeedAction
		} else {
			action = "The full issue body is inlined below, between the markers; work from it " +
				"directly and do not re-fetch the URL. Image markup has been stripped out. Skim the " +
				"comment thread at that URL only for later context."
			inline = "\n\n----- issue body (inlined; media stripped) -----\n" + stripped + "\n----- end issue body -----"
		}
	}

	seed := fmt.Sprintf(
		"Work on Forgejo issue %s (%q).\n\n"+
			"URL: %s\n\n"+
			"%s Then carry it end to end per your container doctrine - "+
			"implement, commit, merge to main, push - and close the issue with a commit "+
			"trailer: closes #%d.",
		ref, title, ref.url(), action, ref.Number)
	if details = strings.TrimSpace(details); details != "" {
		seed += fmt.Sprintf(
			"\n\nOperator note (added at dispatch via --details; treat it as authoritative and "+
				"let it override the issue text where they conflict):\n%s",
			details)
	}
	// A --repo grant widens "done" to every granted repo, verified landed (ward#291).
	seed += grantedRepoDoneClause(extra)
	// Front-load the subsystem context this issue names (ward#236): hand the
	// matching skill/doc paths over up front instead of trusting lazy discovery.
	if block := subsystemSeedBlock(ref, title, body); block != "" {
		seed += "\n\n" + block
	}
	// A headless run detaches with no human watching, so ask it to close with a
	// short retrospective comment - the only voice it leaves behind (ward#281).
	if headless {
		seed += "\n\n" + headlessReflectionAction
	}
	return seed + inline
}

// agentModes is the ordered set of harnesses ward can drive (claude is the default
// --driver); it is the source of truth for the --driver choices (ward#185).
var agentModes = []containerMode{modeClaude, modeCodex, modeQwen, modeGoose}

// agentDriverChoices renders the supported --driver values as a pipe list, e.g.
// "claude|codex|qwen|goose", for flag usage and error text.
func agentDriverChoices() string {
	names := make([]string, 0, len(agentModes))
	for _, m := range agentModes {
		names = append(names, string(m))
	}
	return strings.Join(names, "|")
}

// agentDriverFlag selects the harness driving a surface, defaulting to claude so
// the short path `ward agent engineer <ref>` still works (ward#185). See docs/agent.md.
func agentDriverFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "driver",
		Value: string(modeClaude),
		Usage: "harness that drives the work: " + agentDriverChoices() + " (default claude)",
	}
}

// hostNetFlag is the opt-in network escalation (ward#330): join a carry to the host
// network for a tailnet route, mirroring --aws and implying it. docs/agent-host-net.md.
func hostNetFlag() cli.Flag {
	return &cli.BoolFlag{
		Name: "host-net",
		Usage: "join the container to the host network (--network=host) so it inherits the host's " +
			"tailnet route to tailnet-only hosts like kai-tower-3026; implies --aws (the tower FQDN is " +
			"SSM-only). Drops the cwd-only least-access isolation; off by default (ward#330)",
	}
}

// tsSidecarFlag is the Docker Desktop sibling of --host-net (ward#333): a userspace
// tailscale SOCKS5 sidecar for tailnet reach. Implies --aws; excludes --host-net.
func tsSidecarFlag() cli.Flag {
	return &cli.BoolFlag{
		Name: "ts-sidecar",
		Usage: "run a userspace tailscale SOCKS5 sidecar next to the carry and route tailnet-only hosts " +
			"like kai-tower-3026 through it - the Docker Desktop path where --host-net can't reach the " +
			"tailnet (the LinuxKit VM is not a tailnet node); implies --aws (the auth key + tower IP are " +
			"SSM-only); mutually exclusive with --host-net; off by default (ward#333)",
	}
}

// agentDriver resolves the --driver flag to a containerMode (defaulting to
// claude), erroring on an unknown harness with a --driver-shaped message.
func agentDriver(c *cli.Command) (containerMode, error) {
	m, err := parseMode(c.String("driver"))
	if err != nil {
		return "", fmt.Errorf("invalid --driver %q: want %s", c.String("driver"), agentDriverChoices())
	}
	return m, nil
}

// agentCmdline renders the canonical `ward agent <surface> --driver <mode>` form
// (ward#185) for labels, provenance lines, and the re-dispatch hints ward prints.
func agentCmdline(mode containerMode, surface string) string {
	return fmt.Sprintf("ward agent %s --driver %s", surface, mode)
}

// agentCommand is the `ward agent` umbrella the `warded` public face fronts
// (ward#247, ward#282); a bare ref dispatches the default engineer carry (ward#347).
func agentCommand() *cli.Command {
	return &cli.Command{
		Name:  "agent",
		Usage: "Send an agent into a fresh ephemeral container to carry a Forgejo issue end to end (a bare ref runs the engineer carry).",
		Description: `agent is the issue-carrying dispatcher (the spelling 'warded' fronts), a
roster of startup roles (ward#347): you do not invoke a mode, you send in a
role. Pick a role (engineer|architect|director|advisor) and --driver picks the
harness (claude|codex|qwen|goose, default claude). A BARE REF with no role word
runs the 'engineer' carry - the fire-and-forget default. A bare #N (or N) infers
the owner/repo from the cwd's git origin; owner/repo#N and a full Forgejo issue
URL also work. One line replaces a full container bring-up stack plus a prompt.

  warded coilyco-flight-deck/ward#98          # bare ref -> engineer carry (warded face)
  warded #98                                  # owner/repo inferred from the cwd
  warded engineer #98                         # implement a ticket: detached fire-and-forget
  warded engineer #98 --watch                 # interactive: attach and pair (-w)
  warded engineer "fix the flaky exec_gate test" # freeform -> file an issue first, then carry
  warded engineer #98 --driver codex          # pick another harness
  warded architect                            # read-only interactive session, scope + dispatch
  warded director --repo coilyco-flight-deck/ward # autonomous backlog supervisor
  warded advisor #98 "what would it take to..."   # research the issue, post the answer
  warded advisor "how is the audit log written?"  # answer a freeform question inline
  ward agent engineer coilyco-flight-deck/ward#98 # the canonical spelling warded fronts
  ward agent #98 --print                      # resolve + show the plan, run nothing

See docs/agent.md for the warded face and docs/container.md for the container
model (ephemeral, fresh-clone-inside, reaper-backed). The agent runs under the
container's bypassPermissions policy, so a carry is only accepted against a
trusted owner.`,
		// The umbrella carries the engineer flag set + a default-role action so a
		// bare ref (the warded face, ward#282) runs the engineer carry; empty shows help.
		Flags:  agentEngineerFlags(),
		Action: agentDefaultSurfaceAction(),
		Commands: []*cli.Command{
			agentEngineerCommand(),
			agentArchitectCommand(),
			agentDirectorCommand(),
			agentAdvisorCommand(),
		},
	}
}

// agentDefaultSurfaceAction is the umbrella default: empty shows help, a parseable ref
// runs the engineer carry, any other bare word errors as unknown (ward#282, ward#347).
func agentDefaultSurfaceAction() cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() == 0 {
			return cli.ShowSubcommandHelp(c)
		}
		arg := strings.TrimSpace(c.Args().First())
		if _, err := parseAgentIssueRef(arg); err != nil {
			return fmt.Errorf("unknown command %q for 'ward agent' (roles: engineer, architect, director, advisor); "+
				"a bare ref like #98 or owner/repo#N runs the engineer carry, and freeform work goes to "+
				"`ward agent engineer \"<instructions>\"`", arg)
		}
		return agentEngineerAction()(ctx, c)
	}
}

// agentSurfaceFlags builds the launch flag set shared by work/headless and the
// bare-ref default; headless toggles detach-only vs interactive flags (ward#282).
func agentSurfaceFlags(headless bool) []cli.Flag {
	flags := []cli.Flag{
		agentDriverFlag(),
		&cli.StringFlag{Name: "branch", Usage: "feature branch to create inside the clone (default: issue-<N>)"},
		&cli.StringSliceFlag{Name: "repo", Aliases: []string{"with-repo"}, Usage: "grant the agent an additional writable repo to clone + operate against (owner/name; repeatable). Cloned as a full feature copy under /workspace alongside the issue's repo (ward#230, ward#280; --with-repo is the legacy alias)."},
		&cli.StringFlag{Name: "details", Usage: "extra operator instructions woven into the seeded prompt + pre-flight read (overrides the issue text on conflict)"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault, Sources: cli.EnvVars(envAgentImage), Usage: "dev-base image to run (env: WARD_AGENT_IMAGE)"},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Sources: cli.EnvVars(envAgentTag), Usage: "image tag (env: WARD_AGENT_TAG)"},
		&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
		&cli.StringFlag{Name: "ward-version", Sources: cli.EnvVars(envAgentVersion), Usage: "ward release the container downloads (default: this host's ward; env: WARD_AGENT_VERSION)"},
		&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
		hostNetFlag(),
		tsSidecarFlag(),
		&cli.BoolFlag{Name: "print", Usage: "resolve the issue + seeded prompt + docker plan and exit; inject no push token, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
		&cli.BoolFlag{Name: "force", Usage: "skip the local + remote concurrency reservation checks (reclaim a stale or foreign hold)"},
		&cli.BoolFlag{Name: "go-bootstrap", Usage: "EXPERIMENTAL (ward#181): after ward installs, delegate to the Go 'ward container bootstrap' instead of the bash entrypoint logic. Requires ward in-container - use --ward-source until the image bakes it."},
	}
	if !headless {
		// headless always detaches, so only the interactive surface exposes --detach.
		flags = append(flags, &cli.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "run detached instead of interactive"})
		// --new-tab spawns the work into its own Warp tab (the sidequest path,
		// ward#174); only the interactive work surface offers it.
		flags = append(flags, agentTabFlags()...)
	} else {
		// headless gets an autonomous pre-flight before detaching (ward#137,
		// ward#147; see docs/agent.md); --no-preflight skips it.
		flags = append(flags, &cli.BoolFlag{Name: "no-preflight", Usage: "skip the pre-flight feasibility check and detach immediately"})
	}
	return flags
}

// resolvedWork bundles resolveAgentWork's output: ref, title, body, comment thread
// (ward#154), the --details note (ward#167), and the seeded prompt.
type resolvedWork struct {
	Ref      agentIssueRef
	Title    string
	Body     string
	Comments []issueComment
	Details  string
	Seed     string
	// ExtraRepos are the --repo grants the run also clones writable (ward#230);
	// the pre-flight must hear about them or it false-NO-GOs cross-repo work (ward#266).
	ExtraRepos []targetRepo
}

// resolveAgentWork parses + trust-gates the ref, fetches the issue (failing fast
// before any container spins), and returns the ref, title, body, and seed prompt.
func (r *Runner) resolveAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string, headless bool) (resolvedWork, error) {
	label := agentCmdline(mode, surface)
	ref, err := r.resolveAgentIssueRef(ctx, c.Args().First())
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
	details := strings.TrimSpace(c.String("details"))
	// Fetch comments so the pre-flight sees decisions made there, not just the
	// body (ward#154); degrade to a body-only read on failure (the prior behavior).
	comments, cerr := r.fetchIssueComments(ctx, ref)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "%s: note: could not read comments on %s (%v); pre-flight reads the body only\n", label, ref, cerr)
	}
	// Resolve the --repo grants now so the pre-flight knows the run gets these repos
	// too ("with-repo" is the shared lookup key; ward#266, ward#280).
	extra, eerr := parseExtraRepos(c.StringSlice("with-repo"), targetRepo{Owner: ref.Owner, Name: ref.Repo})
	if eerr != nil {
		return resolvedWork{}, fmt.Errorf("%s: %w", label, eerr)
	}
	// headless detaches fire-and-forget, so its seed gets the closing reflection
	// (ward#281); an interactive --watch carry has a human watching and skips it.
	return resolvedWork{Ref: ref, Title: title, Body: issue.Body, Comments: comments, Details: details, ExtraRepos: extra, Seed: agentSeedPrompt(ref, title, issue.Body, details, mode, headless, extra)}, nil
}

// fetchIssueComments returns the comment thread (oldest first) for the pre-flight
// read via the host Forgejo client; caller degrades gracefully on error.
func (r *Runner) fetchIssueComments(ctx context.Context, ref agentIssueRef) ([]issueComment, error) {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return nil, err
	}
	return cl.listIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
}

// runAgentWork resolves the issue, seeds the prompt, runs the autonomous
// pre-flight for interactive headless runs (runPreflight), and launches.
func (r *Runner) runAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string, headless bool) error {
	w, err := r.resolveAgentWork(ctx, c, mode, surface, headless)
	if err != nil {
		return err
	}
	// --new-tab spawns the work into its own Warp tab (the sidequest path) rather
	// than launching here; the ref is already validated. See docs/agent.md, ward#174.
	if !headless && c.Bool("new-tab") {
		return r.runAgentNewTab(ctx, c, mode, w)
	}
	// Warn at host dispatch if ward is stale; a detached run buries the only
	// `ward version` signal in a container log (ward#143). --print stays offline.
	if !c.Bool("print") {
		r.maybeWarnWardOutdated(ctx)
	}
	if headless && preflightWanted(c) {
		// ward#184: gate on the cheap, authoritative reservation before a full LLM
		// pre-flight is spent on an issue another run holds. See docs/agent.md.
		if perr := r.precheckReservation(ctx, agentCmdline(mode, surface), w, c.Bool("force")); perr != nil {
			return perr
		}
		proceed, perr := r.runPreflight(ctx, mode, surface, w)
		if perr != nil {
			return fmt.Errorf("%s: pre-flight: %w", agentCmdline(mode, surface), perr)
		}
		if !proceed {
			// runPreflight already reported the NO-GO and posted the issue comment.
			return nil
		}
	}
	return r.launchAgentContainer(ctx, c, mode, surface, headless, w.Ref, w.Title, w.Seed)
}

// preflightTimeout caps the pre-flight read so a wedged agent can't hold the
// operator's terminal hostage before the real run even starts.
const preflightTimeout = 3 * time.Minute

// preflightWanted gates the pre-flight to an interactive dispatch (a human at the
// terminal who walked away), never --print, honoring --no-preflight. See docs.
func preflightWanted(c *cli.Command) bool {
	return terminalAttached() && !c.Bool("print") && !c.Bool("no-preflight")
}

// preflightPrompt asks the about-to-detach agent for a feasibility read ending on a
// GO / NO-GO line, feeding --details, comments, and --repo grants (ward#266).
func preflightPrompt(ref agentIssueRef, title, body, details string, comments []issueComment, extra []targetRepo) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "(untitled)"
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "(no description provided)"
	}
	note := ""
	if details = strings.TrimSpace(details); details != "" {
		note = fmt.Sprintf(
			"\n\nThe operator also attached this steering note at dispatch (--details), which the "+
				"detached run will treat as authoritative over the issue text - weigh it in your read:\n%s",
			details)
	}
	thread := preflightComments(comments)
	if thread == "" {
		thread = "(no comments yet)"
	}
	// Name the --repo grants in the prompt or the read false-NO-GOs cross-repo
	// work as unreachable from "a ward-only clone" - the exact ward#266 failure.
	cloneScope := fmt.Sprintf("a FRESH CLONE of %s/%s", ref.Owner, ref.Repo)
	extraNote := ""
	if len(extra) > 0 {
		slugs := make([]string, len(extra))
		for i, repo := range extra {
			slugs[i] = repo.slug()
		}
		joined := strings.Join(slugs, ", ")
		cloneScope = fmt.Sprintf("FRESH CLONES of %s/%s AND of %s", ref.Owner, ref.Repo, joined)
		extraNote = fmt.Sprintf(
			"\n\nThis dispatch GRANTED EXTRA REPOS via --repo: %s. Each lands as a full, "+
				"WRITABLE working copy under /workspace beside the issue's repo, so cross-repo work "+
				"spanning them - creating a package in one, moving code across the boundary, wiring "+
				"the seams, landing a coordinated change - is squarely in scope for this run. Do NOT "+
				"answer NO-GO or WRONG-REPO merely because the deliverable lands in one of these "+
				"granted repos (%s) rather than %s/%s; you will have all of them in hand.",
			joined, joined, ref.Owner, ref.Repo)
	}
	gate := subsystemPreflightBlock(ref, title, body)
	return fmt.Sprintf(
		"You are about to be sent, fire-and-forget, into an ephemeral container to carry "+
			"this Forgejo issue end to end on your own - implement, commit, merge to main, "+
			"push - with no human watching once you detach.\n\n"+
			"That detached run happens in %s pulled inside the container. "+
			"The directory you are reading this in right now is unrelated host scratch - it may "+
			"hold a different repo, or none at all. So judge feasibility from the issue text "+
			"alone, never from the local working tree: a file, path, or package that looks "+
			"missing in the current directory tells you nothing about the clone you will actually "+
			"get, so do not conclude the issue is mis-filed just because the local tree lacks it.%s\n\n"+
			"Before that detached run starts, give a quick PRE-FLIGHT read: based on the issue "+
			"AND its comment thread below, do you think you can carry it to merge unattended? "+
			"Later comments can supersede the original description - the author may have answered "+
			"an open question or picked among options there, so weigh the latest word, not just "+
			"the initial framing.\n\n"+
			"Issue: %s (%q)\n\n%s%s\n\n"+
			"Comment thread (oldest first):\n\n%s%s\n\n"+
			"Before the verdict line, add a \"Context to front-load:\" line that names the "+
			"conventions and subsystems this work touches (the schemas, file layouts, and wiring "+
			"you will need to know), and confirm you will READ each one in the clone before your "+
			"first edit, not discover it lazily mid-task. Naming a gap is not closing it: a "+
			"convention you can only locate is still unread. If there are none, say so explicitly.\n\n"+
			"Then answer in 2-4 sentences naming the main risk or unknown, then a final line of "+
			"exactly one of:\n"+
			"  \"GO\" - you would take it on unattended;\n"+
			"  \"NO-GO: <reason>\" - a human should weigh in first;\n"+
			"  \"WRONG-REPO: owner/repo - <what to file there>\" - the work plainly belongs in a "+
			"different repo than %s/%s. Only say this when the issue text alone makes it obvious - "+
			"do not go digging to decide it, and never from files missing in the current directory. "+
			"ward will blind-file a fresh issue in that repo and launch nothing here.\n"+
			"This is a judgment call, not a commitment - be honest about ambiguity.",
		cloneScope, extraNote, ref, title, body, note, thread, gate, ref.Owner, ref.Repo)
}

// preflightComments renders the human comment thread (oldest first) for the
// pre-flight, dropping ward's own bookkeeping so only human words sway it (see docs).
func preflightComments(comments []issueComment) string {
	var b strings.Builder
	for _, c := range comments {
		body := strings.TrimSpace(c.Body)
		if body == "" || strings.Contains(c.Body, agentReservationMarker) || strings.Contains(c.Body, preflightNoGoMarker) {
			continue
		}
		who := strings.TrimSpace(c.User.Login)
		if who == "" {
			who = "(unknown author)"
		}
		fmt.Fprintf(&b, "--- comment by %s (%s) ---\n%s\n\n", who, c.CreatedAt.Format(time.RFC3339), body)
	}
	return strings.TrimSpace(b.String())
}

// capturePreflight runs the feasibility-read argv in a fresh empty temp dir so the
// read never inherits the dispatch cwd (ward#169; see docs/agent.md pre-flight).
func (r *Runner) capturePreflight(ctx context.Context, argv []string) ([]byte, error) {
	// No temp dir means no isolation, but the prompt lever still stands: fall back
	// to a plain cwd capture rather than strand a workable issue behind flakiness.
	dir, err := os.MkdirTemp("", "ward-preflight-*")
	if err != nil {
		return r.Runner.Capture(ctx, argv[0], argv[1:]...)
	}
	defer os.RemoveAll(dir)
	return r.captureInDir(ctx, dir, argv[0], argv[1:]...)
}

// captureInDir runs Capture with the process cwd temporarily set to dir, restored
// afterward (cli-guard's Capture has no Dir knob). A guarded chdir is safe here.
func (r *Runner) captureInDir(ctx context.Context, dir, bin string, argv ...string) ([]byte, error) {
	// The pre-flight is a sequential host one-shot, so no concurrent cwd user can
	// race this; a failed Getwd/Chdir simply no-ops to a plain cwd capture.
	if prev, err := os.Getwd(); err == nil {
		if cerr := os.Chdir(dir); cerr == nil {
			defer os.Chdir(prev) //nolint:errcheck // best-effort restore
		}
	}
	return r.Runner.Capture(ctx, bin, argv...)
}

// runPreflight acts on the agent's feasibility verdict with no human, shared by
// the headless + task surfaces (ward#147, ward#149): only NO-GO blocks. See docs.
func (r *Runner) runPreflight(ctx context.Context, mode containerMode, surface string, w resolvedWork) (bool, error) {
	label := agentCmdline(mode, surface)
	bin := mode.agentBinary()
	argv, ok := mode.hostPreflightArgv(preflightPrompt(w.Ref, w.Title, w.Body, w.Details, w.Comments, w.ExtraRepos))
	// No host self-assessment (claude+goose have one, codex/qwen don't) or no
	// binary on PATH: can't fairly bounce the issue, so the dispatch proceeds.
	if !ok || !hostHasBinary(bin) {
		fmt.Fprintf(os.Stderr, "%s: %s self-assessment unavailable on this host; proceeding with the detached run.\n", label, bin)
		return true, nil
	}

	fmt.Fprintf(os.Stderr, "%s: pre-flight - asking %s whether it can carry %s before detaching...\n\n", label, bin, w.Ref)
	pctx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()
	// Capture (not Exec) so ward can read the verdict; the read is echoed below.
	// capturePreflight isolates it in a neutral dir, never the dispatch cwd (#169).
	out, err := r.capturePreflight(pctx, argv)
	read := strings.TrimSpace(string(out))
	if read != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", read)
	}
	if err != nil {
		// A read that didn't complete is not the agent saying no: fail open so a
		// flaky host agent never strands an otherwise-workable issue.
		fmt.Fprintf(os.Stderr, "%s: pre-flight read did not complete (%v); proceeding with the detached run.\n", label, err)
		return true, nil
	}

	switch outcome := parsePreflightVerdict(read); outcome.Verdict {
	case verdictWrongRepo:
		// WRONG-REPO always launches nothing here (it either blind-fires elsewhere
		// or bounces to a human), so proceed is false regardless of the error.
		return false, r.handlePreflightWrongRepo(ctx, mode, surface, w, outcome, read)
	case verdictNoGo:
		fmt.Fprintf(os.Stderr, "%s: pre-flight NO-GO for %s; launching nothing, commenting on the issue.\n", label, w.Ref)
		if cerr := r.postPreflightNoGo(ctx, mode, surface, w.Ref, outcome.Reason, read); cerr != nil {
			return false, fmt.Errorf("post NO-GO comment on %s: %w", w.Ref, cerr)
		}
		fmt.Fprintf(os.Stderr, "%s: commented NO-GO on %s - %s\n", label, w.Ref, w.Ref.url())
		return false, nil
	case verdictUnknown, verdictGo:
		// No clear verdict line or an explicit GO: proceed with the detached run.
		return true, nil
	default:
		return true, nil
	}
}

// wrongRepoBounceReason builds the human-facing reason a WRONG-REPO verdict is
// unusable: no usable owner/repo, the issue's own repo, or an untrusted owner.
func wrongRepoBounceReason(outcome preflightOutcome, target targetRepo, orgs []string, ok, sameRepo bool) string {
	reason := outcome.Reason
	if reason == "" {
		reason = "agent flagged this as belonging in another repo"
	}
	switch {
	case !ok:
		return "agent flagged WRONG-REPO but named no usable owner/repo: " + reason
	case sameRepo:
		return "agent flagged WRONG-REPO but named this same repo: " + reason
	default:
		return fmt.Sprintf("agent routed this to untrusted repo %s (not in %s): %s",
			target.slug(), strings.Join(orgs, ", "), reason)
	}
}

// wrongRepoTarget splits a parsed WRONG-REPO "owner/repo" into a targetRepo,
// failing only on an empty/half target (callers treat that as a NO-GO).
func wrongRepoTarget(s string) (targetRepo, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok || owner == "" || name == "" {
		return targetRepo{}, false
	}
	return targetRepo{Owner: owner, Name: name}, true
}

// handlePreflightWrongRepo acts on a WRONG-REPO verdict (ward#159): blind-fire
// into a trusted target repo, else bounce to a human (always launches nothing).
func (r *Runner) handlePreflightWrongRepo(ctx context.Context, mode containerMode, surface string, w resolvedWork, outcome preflightOutcome, read string) error {
	label := agentCmdline(mode, surface)
	target, ok := wrongRepoTarget(outcome.Repo)
	sameRepo := ok && target.Owner == w.Ref.Owner && target.Name == w.Ref.Repo
	// An untrusted repo, the issue's own repo, or a half target is no blind-fire
	// target: bounce to a human rather than guessing.
	if !ok || sameRepo || !r.ownerAllowed(target.Owner) {
		reason := wrongRepoBounceReason(outcome, target, r.primaryOrgs(), ok, sameRepo)
		fmt.Fprintf(os.Stderr, "%s: pre-flight WRONG-REPO unusable for %s; bouncing to a human.\n", label, w.Ref)
		if cerr := r.postPreflightNoGo(ctx, mode, surface, w.Ref, reason, read); cerr != nil {
			return fmt.Errorf("post NO-GO comment on %s: %w", w.Ref, cerr)
		}
		return nil
	}

	fmt.Fprintf(os.Stderr, "%s: pre-flight WRONG-REPO for %s -> %s; blind-firing an issue there, launching nothing.\n", label, w.Ref, target.slug())
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return err
	}
	signed := cl.withMode(mode)
	number, err := signed.createIssue(ctx, target.Owner, target.Name,
		w.Title, blindfireIssueBody(mode, surface, w, outcome.Reason))
	if err != nil {
		return fmt.Errorf("blind-fire issue into %s: %w", target.slug(), err)
	}
	filed := agentIssueRef{Owner: target.Owner, Repo: target.Name, Number: number}
	fmt.Fprintf(os.Stderr, "%s: blind-fired %s - %s\n", label, filed, filed.url())
	// Point the original issue at the freshly-filed one so the trail is visible.
	if cerr := signed.commentIssue(ctx, w.Ref.Owner, w.Ref.Repo, w.Ref.Number,
		preflightWrongRepoComment(mode, surface, filed, outcome.Reason, read)); cerr != nil {
		return fmt.Errorf("comment WRONG-REPO routing on %s: %w", w.Ref, cerr)
	}
	fmt.Fprintf(os.Stderr, "%s: noted the routing on %s - %s\n", label, w.Ref, w.Ref.url())
	return nil
}

// hostHasBinary reports whether bin resolves on the host PATH.
func hostHasBinary(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// preflightVerdict is ward's read of the agent's pre-flight self-assessment.
type preflightVerdict int

const (
	verdictUnknown   preflightVerdict = iota // no clear verdict line - treated as proceed
	verdictGo                                // an explicit GO
	verdictNoGo                              // an explicit NO-GO (carries a reason)
	verdictWrongRepo                         // an explicit WRONG-REPO (carries a target repo + reason)
)

var (
	// preflightWrongRepoRE matches a WRONG-REPO line (hyphen, space, or run-together),
	// capturing the owner/repo target then the reason; checked before the NO-GO form.
	preflightWrongRepoRE = regexp.MustCompile(`(?i)^wrong[-\s]?repo\b[\s:.\-–—]*([A-Za-z0-9._-]+/[A-Za-z0-9._-]+)\b[\s:.\-–—]*(.*)$`)
	// preflightNoGoRE matches a verdict line opening with NO-GO (hyphen, space, or
	// run-together) and captures the trailing reason. Checked before the GO form.
	preflightNoGoRE = regexp.MustCompile(`(?i)^no[-\s]?go\b[\s:.\-–—]*(.*)$`)
	// preflightGoRE matches a bare GO verdict line; the prompt asks for exactly GO,
	// so an inline "...go ahead" never trips it.
	preflightGoRE = regexp.MustCompile(`(?i)^go\b[\s.!]*$`)
)

// preflightOutcome is ward's parsed read of the verdict line: the verdict, an
// optional reason, and a WRONG-REPO target as owner/repo (empty otherwise).
type preflightOutcome struct {
	Verdict preflightVerdict
	Reason  string
	Repo    string
}

// parsePreflightVerdict reads the agent's final GO / NO-GO / WRONG-REPO line,
// tolerating decoration; the last verdict line wins. See docs/agent.md.
func parsePreflightVerdict(read string) preflightOutcome {
	out := preflightOutcome{Verdict: verdictUnknown}
	for _, raw := range strings.Split(read, "\n") {
		s := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "*_`>#-•·"))
		if s == "" {
			continue
		}
		if m := preflightWrongRepoRE.FindStringSubmatch(s); m != nil {
			out = preflightOutcome{Verdict: verdictWrongRepo, Repo: m[1], Reason: strings.TrimSpace(m[2])}
			continue
		}
		if m := preflightNoGoRE.FindStringSubmatch(s); m != nil {
			out = preflightOutcome{Verdict: verdictNoGo, Reason: strings.TrimSpace(m[1])}
			continue
		}
		if preflightGoRE.MatchString(s) {
			out = preflightOutcome{Verdict: verdictGo}
		}
	}
	return out
}

// postPreflightNoGo comments the NO-GO verdict back on the issue (host Forgejo
// client, SSM-backed token), bouncing it to a human instead of failing silently.
func (r *Runner) postPreflightNoGo(ctx context.Context, mode containerMode, surface string, ref agentIssueRef, reason, read string) error {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return err
	}
	return cl.withMode(mode).commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, preflightNoGoComment(mode, surface, reason, read))
}

// preflightNoGoMarker tags every NO-GO comment so a later pre-flight read can
// drop ward's own prior verdicts from the thread it weighs (ward#154).
const preflightNoGoMarker = "<!-- ward-preflight-nogo -->"

// preflightNoGoComment renders the NO-GO issue comment: reason, why nothing
// launched, how to re-dispatch, the surface (headless|task), and the read. Pure.
func preflightNoGoComment(mode containerMode, surface, reason, read string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "(no reason given)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### 🛫 ward pre-flight: NO-GO\n\n")
	fmt.Fprintf(&b, "`%s` ran a pre-flight feasibility read on this issue before "+
		"detaching a fire-and-forget run, and the agent judged it **NO-GO** - it should not be carried "+
		"unattended until a human weighs in.\n\n", agentCmdline(mode, surface))
	fmt.Fprintf(&b, "> %s\n\n", reason)
	// Re-dispatch points at the `engineer` carry: the issue is already filed, so a
	// freeform engineer run would file a duplicate (ward#347).
	fmt.Fprintf(&b, "No container was launched. Review the issue (clarify the scope, resolve the unknown, "+
		"or split it), then re-dispatch - `%s <ref> --no-preflight` skips this gate "+
		"once you've decided it's good to go.\n", agentCmdline(mode, "engineer"))
	if read = strings.TrimSpace(read); read != "" {
		fmt.Fprintf(&b, "\n<details><summary>full pre-flight read</summary>\n\n%s\n\n</details>\n", read)
	}
	fmt.Fprintf(&b, "\n---\nPosted automatically by `%s` pre-flight (ward#147, ward#149).\n%s", agentCmdline(mode, surface), preflightNoGoMarker)
	return b.String()
}

// blindfireIssueBody renders the WRONG-REPO blind-fire body (ward#159): source
// text verbatim + reason + provenance, reusing the read so it costs no cycles.
func blindfireIssueBody(mode containerMode, surface string, w resolvedWork, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "(no reason given)"
	}
	body := strings.TrimSpace(w.Body)
	if body == "" {
		body = "(the source issue had no description)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Routed here from %s by `%s` pre-flight (ward#159): the feasibility "+
		"read judged this work belongs in this repo, not %s/%s.\n\n", w.Ref, agentCmdline(mode, surface), w.Ref.Owner, w.Ref.Repo)
	fmt.Fprintf(&b, "> %s\n\n", reason)
	fmt.Fprintf(&b, "This was filed blind from the source issue's text - nobody searched this repo first, "+
		"so confirm it fits before working it.\n\n")
	fmt.Fprintf(&b, "---\n### Source issue (%s)\n\n%s\n", w.Ref, body)
	fmt.Fprintf(&b, "\n---\nFiled automatically by `%s` pre-flight (ward#159).", agentCmdline(mode, surface))
	return b.String()
}

// preflightWrongRepoComment renders the note left on the original issue after a
// blind-fire: where the work was routed, why, and the read. Mirrors the NO-GO form.
func preflightWrongRepoComment(mode containerMode, surface string, filed agentIssueRef, reason, read string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "(no reason given)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### 🎯 ward pre-flight: WRONG-REPO\n\n")
	fmt.Fprintf(&b, "`%s` ran a pre-flight read on this issue and judged the work "+
		"belongs in **%s**, not here. Rather than burn cycles searching, it blind-fired a fresh "+
		"issue there:\n\n", agentCmdline(mode, surface), filed.repoSlug())
	fmt.Fprintf(&b, "- %s - %s\n\n", filed, filed.url())
	fmt.Fprintf(&b, "> %s\n\n", reason)
	fmt.Fprintf(&b, "No container was launched here. If the routing is wrong, close %s and re-dispatch "+
		"this issue with `%s <ref> --no-preflight` to skip the gate.\n", filed, agentCmdline(mode, "engineer"))
	if read = strings.TrimSpace(read); read != "" {
		fmt.Fprintf(&b, "\n<details><summary>full pre-flight read</summary>\n\n%s\n\n</details>\n", read)
	}
	fmt.Fprintf(&b, "\n---\nPosted automatically by `%s` pre-flight (ward#159).", agentCmdline(mode, surface))
	return b.String()
}

// buildAgentPlan composes the container plan (seeded argv, issue-<N> branch, named
// container) for a resolved issue. detached strips TTY flags so it never grabs a pty.
func buildAgentPlan(c *cli.Command, mode containerMode, ref agentIssueRef, seed string, headless, detached bool, assetsDir string) (upPlan, error) {
	cwd := resolveInvokeCWD()
	if cwd == "" {
		return upPlan{}, fmt.Errorf("cannot resolve the current directory")
	}
	repo := targetRepo{Owner: ref.Owner, Name: ref.Repo}
	plan, err := buildUpPlan(c, repo, mode, cwd, assetsDir, []string{seed})
	if err != nil {
		return upPlan{}, err
	}
	if plan.Branch == "" {
		plan.Branch = fmt.Sprintf("issue-%d", ref.Number)
	}
	// Override the generic ward-<repo>-<rand> name with one that names the issue
	// and harness, so a host running several agents can tell them apart.
	plan.Name = agentContainerName(repo, mode, ref.Number, randHex())
	// Carry the issue number into the container so the reaper can release the
	// reservation this run took if the container dies pre-launch (ward#264).
	plan.Issue = ref.Number
	plan.Headless = headless
	if detached {
		plan.Interactive = false
		plan.TTY = false
	}
	return plan, nil
}

// carryingLine renders the one-line "what am I about to work on" echo (ward#307):
// label, ref, title - returning "" for an empty title so a seedless run stays quiet.
func carryingLine(label string, ref agentIssueRef, title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	return fmt.Sprintf("%s: carrying %s - %q", label, ref, t)
}

// launchAgentContainer turns a resolved (ref, title, seed) into the container
// plan and fires it - the shared tail of work, headless, and task. See docs/agent.md.
func (r *Runner) launchAgentContainer(ctx context.Context, c *cli.Command, mode containerMode, surface string, headless bool, ref agentIssueRef, title, seed string) error {
	label := agentCmdline(mode, surface)

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

	// Echo the issue title so the operator sees *what* this run carries, not just the
	// opaque ref number - the one line saying the right issue is in flight (ward#307).
	if line := carryingLine(label, ref, title); line != "" {
		fmt.Fprintln(os.Stderr, line)
	}

	// Reserve the issue so another run (file sentinel here, Forgejo marker elsewhere)
	// won't redo it. Detached holds for the container's life; attached releases on return.
	releaseReservation, err := r.reserveIssue(ctx, label, mode, ref, plan.Name, plan.Branch, c.Bool("force"))
	if err != nil {
		return err
	}
	if !detached {
		defer releaseReservation()
	}

	// Reclaim dead containers' writable layers before adding one more, so the
	// agent fleet can't exhaust the docker disk and wedge new launches (ward#272).
	r.sweepStaleContainers(ctx)
	if !c.Bool("no-pull") {
		r.pullAgentImage(ctx, plan, label)
	}
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), r.resolveAgentCreds(ctx, mode))
	if err != nil {
		return err
	}
	defer cleanupEnv()
	return r.createAgentContainer(ctx, plan, envFile)
}

// pullHeartbeatDefault is how often a silenced detached pull beats a "still
// pulling" line so a stall on a slow/mid-push registry stays attributable.
const pullHeartbeatDefault = 30 * time.Second

// pullAgentImage pulls plan.Image: interactive streams docker, detached silences
// it but names the pull + beats a heartbeat (ward#306, ward#322; docs/agent-flags.md).
func (r *Runner) pullAgentImage(ctx context.Context, plan upPlan, label string) {
	var perr error
	if plan.Interactive {
		perr = r.Runner.Exec(ctx, "docker", "pull", plan.Image)
	} else {
		// Capture the live stderr before runDockerSilenced swaps it for
		// io.Discard; the named line and heartbeat must outlive the silencing.
		w := r.Runner.Stderr
		fmt.Fprintf(w, "%s: pulling %s (silenced; this can stall on a mid-push registry)\n", label, plan.Image)
		stop := r.beatPullHeartbeat(w, label, plan.Image)
		perr = r.runDockerSilenced(ctx, true, "pull", plan.Image)
		stop()
	}
	if perr != nil {
		fmt.Fprintf(os.Stderr, "%s: image pull failed (%v); trying the local image\n", label, perr)
	}
}

// beatPullHeartbeat prints a "still pulling" line to w every interval until the
// returned stop func is called, which drains the goroutine first (ward#322).
func (r *Runner) beatPullHeartbeat(w io.Writer, label, image string) func() {
	interval := r.pullHeartbeatInterval
	if interval <= 0 {
		interval = pullHeartbeatDefault
	}
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Fprintf(w, "%s: still pulling %s (%s elapsed; a mid-push registry can be slow)\n",
					label, image, time.Since(start).Round(time.Second))
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// createAgentContainer fires `docker run`: interactive streams to the terminal;
// detached swallows the lone container-id hash docker echoes (ward#306).
func (r *Runner) createAgentContainer(ctx context.Context, plan upPlan, envFile string) error {
	// --host-net only carries the tailnet on a host that is itself a tailnet node;
	// warn loudly when it won't, so a no-op route doesn't read as success (ward#332).
	r.maybeWarnHostNet(plan)
	// The sidecar must exist before the carry's --network=container attaches to it.
	if plan.TSSidecar {
		if err := r.startTSSidecar(ctx, plan); err != nil {
			return err
		}
	}
	if plan.Interactive {
		// Attached: tear the sidecar down on return (detached relies on the sweep).
		if plan.TSSidecar {
			defer r.stopTSSidecar(ctx, plan.Name)
		}
		return r.Runner.Exec(ctx, "docker", dockerCreateArgv(plan, envFile)...)
	}
	if inContainer() {
		// Dispatching from inside a container (e.g. `warded #N` from explore): the
		// daemon can't see this container's host-bind sources, so create + cp + start.
		return r.createDetachedViaCopy(ctx, plan, envFile)
	}
	return r.runDockerSilenced(ctx, false, dockerCreateArgv(plan, envFile)...)
}

// createDetachedViaCopy creates the sibling with volume mounts only, `docker cp`s the
// host-bind sources in, then starts it - host-path-independent dispatch (ward#323).
func (r *Runner) createDetachedViaCopy(ctx context.Context, plan upPlan, envFile string) error {
	out, err := r.captureDockerSilenced(ctx, dockerCreateNoBindsArgv(plan, envFile)...)
	if err != nil {
		return fmt.Errorf("ward container: create sibling: %w", err)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return fmt.Errorf("ward container: docker create returned no container id")
	}
	for _, m := range hostBindMounts(plan) {
		if !pathExists(m.Source) {
			continue // an unset optional bind (e.g. --aws) has no source to copy
		}
		if cerr := r.Runner.Exec(ctx, "docker", "cp", m.Source+"/.", id+":"+m.Target); cerr != nil {
			return fmt.Errorf("ward container: docker cp %s -> %s: %w", m.Source, m.Target, cerr)
		}
	}
	return r.runDockerSilenced(ctx, false, "start", id)
}

// captureDockerSilenced runs docker capturing stdout (the created container id) with
// the CLI hint banner off, so a create's id reads clean (ward#306-style).
func (r *Runner) captureDockerSilenced(ctx context.Context, argv ...string) (string, error) {
	saveEnv := r.Runner.Env
	r.Runner.Env = append(append([]string(nil), saveEnv...), "DOCKER_CLI_HINTS=false")
	defer func() { r.Runner.Env = saveEnv }()
	out, err := r.Runner.Capture(ctx, "docker", argv...)
	return string(out), err
}

// inContainer reports whether ward runs inside a container (the docker /.dockerenv
// marker), where host bind-mount sources don't resolve on the daemon (ward#323).
func inContainer() bool { return fileExists("/.dockerenv") }

// pathExists reports whether a path exists, file or directory (fileExists excludes
// dirs, but bind sources like the assets dir and cwd are directories).
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// runDockerSilenced runs docker with the CLI hint banner off and stdout dropped
// (stderr too when silenceStderr), keeping a detached launch quiet (ward#306).
func (r *Runner) runDockerSilenced(ctx context.Context, silenceStderr bool, argv ...string) error {
	// Launches are sequential per process, so swapping the shared Runner's
	// writers/env around one call and restoring them on return is safe.
	saveOut, saveErr, saveEnv := r.Runner.Stdout, r.Runner.Stderr, r.Runner.Env
	r.Runner.Stdout = io.Discard
	if silenceStderr {
		r.Runner.Stderr = io.Discard
	}
	r.Runner.Env = append(append([]string(nil), saveEnv...), "DOCKER_CLI_HINTS=false")
	defer func() {
		r.Runner.Stdout, r.Runner.Stderr, r.Runner.Env = saveOut, saveErr, saveEnv
	}()
	return r.Runner.Exec(ctx, "docker", argv...)
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
	return fmt.Sprintf("%s\n\n---\nFiled by `%s`.", instructions, agentCmdline(mode, "engineer"))
}

// runAgentTask is the engineer role's freeform mode (ward#347, was `task`): it routes
// to ROUTE or DIRECT (ward#164) by the positional, files an issue, then carries it.
func (r *Runner) runAgentTask(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "engineer")
	route, repoArg, err := classifyTaskInvocation(c.Args().First(), c.String("instructions"), c.String("instructions-file"))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if route {
		return r.runAgentTaskRoute(ctx, c, mode, strings.TrimSpace(c.Args().First()))
	}
	return r.runAgentTaskDirect(ctx, c, mode, repoArg)
}

// runAgentTaskDirect resolves the repo, files an issue from --instructions, and
// runs the headless carry container - today's behavior, unchanged. See docs.
func (r *Runner) runAgentTaskDirect(ctx context.Context, c *cli.Command, mode containerMode, repoArg string) error {
	label := agentCmdline(mode, "engineer")
	repo, _, err := r.resolveTarget(ctx, repoArg)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Same trust gate as the engineer carry: the container runs bypassPermissions, so
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

	// task always detaches, so host dispatch is the last interactive moment - surface
	// a stale-ward reminder before it files+launches (ward#143).
	r.maybeWarnWardOutdated(ctx)

	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	number, err := cl.withMode(mode).createIssue(ctx, repo.Owner, repo.Name, title, body)
	if err != nil {
		return fmt.Errorf("%s: file issue in %s/%s: %w", label, repo.Owner, repo.Name, err)
	}
	ref := agentIssueRef{Owner: repo.Owner, Repo: repo.Name, Number: number}
	fmt.Fprintf(os.Stderr, "%s: filed %s - %s\n", label, ref, ref.url())

	// The freeform engineer run carries headless, so it gets the same pre-flight
	// (ward#149): a NO-GO comments on the just-filed issue and launches nothing.
	if preflightWanted(c) {
		proceed, perr := r.runPreflight(ctx, mode, "engineer", resolvedWork{Ref: ref, Title: title, Body: body})
		if perr != nil {
			return fmt.Errorf("%s: pre-flight: %w", label, perr)
		}
		if !proceed {
			// runPreflight already reported the NO-GO and posted the issue comment.
			return nil
		}
	}

	// The freeform instructions are the filed body (no --details, ward#167); it runs
	// the headless carry, so the seed is headless: inlined body + reflection (#157/#281).
	seed := agentSeedPrompt(ref, title, body, "", mode, true, nil)
	return r.launchAgentContainer(ctx, c, mode, "engineer", true, ref, title, seed)
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
	seed := agentSeedPrompt(previewRef, title, body, "", mode, true, nil)
	plan, err := buildUpPlan(c, repo, mode, "", "", []string{seed})
	if err != nil {
		return err
	}
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
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(mode, "engineer"))
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
	_, werr := io.WriteString(out, b.String())
	return werr
}

// ownerAllowed reports whether owner is in ward's primary-org trust set, via
// cli-guard's pkg/ownertrust (ward supplies the accepted set).
func (r *Runner) ownerAllowed(owner string) bool {
	return ownertrust.List{Extra: r.primaryOrgs()}.Allowed(owner)
}

// printAgentPlan renders the resolved issue, the seeded prompt, and the docker
// plan without firing - the dry-run preview, safe with no docker daemon.
func printAgentPlan(c *cli.Command, p upPlan, ref agentIssueRef, title, seed, surface string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(p.Mode, surface))
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
	if p.TSSidecar {
		// The carry joins this sidecar's netns, so it is brought up first (ward#333).
		fmt.Fprintf(&b, "docker %s\n", strings.Join(tsSidecarRunArgv(p.Name, p.Repo.slug(), "<ward-ts-authkey-envfile>"), " "))
	}
	fmt.Fprintf(&b, "docker %s\n", strings.Join(dockerCreateArgv(p, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, b.String())
	return err
}
