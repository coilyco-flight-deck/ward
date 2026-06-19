package main

import (
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

// repoSlug renders the owner/repo pair without the issue number.
func (r agentIssueRef) repoSlug() string {
	return r.Owner + "/" + r.Repo
}

// url renders the canonical Forgejo issue URL for the seeded prompt.
func (r agentIssueRef) url() string {
	return fmt.Sprintf("%s/%s/%s/issues/%d", strings.TrimRight(forgejoBaseURL, "/"), r.Owner, r.Repo, r.Number)
}

// agentRefTrailerRE swallows an optional trailing slash plus any appended
// ?query and/or #fragment, so a browser-copied ref parses unedited (see docs).
const agentRefTrailerRE = `/?(?:[?#].*)?$`

// agentIssueShortRE matches owner/repo#N, ignoring any appended query/fragment.
var agentIssueShortRE = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)#(\d+)` + agentRefTrailerRE)

// agentIssueURLRE matches <forgejoBaseURL>/owner/repo/issues/N, query/fragment
// ignored. A follow-up unifies this with cli-guard dispatch.parseIssueRef.
var agentIssueURLRE = regexp.MustCompile(`^` + regexp.QuoteMeta(strings.TrimRight(forgejoBaseURL, "/")) +
	`/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)/issues/(\d+)` + agentRefTrailerRE)

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

// agentSeedPrompt seeds the agent: the issue, a mode-aware first move (ward#157),
// and an optional authoritative --details note (ward#167). See docs/agent.md.
func agentSeedPrompt(ref agentIssueRef, title, body, details string, mode containerMode) string {
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
	return seed + inline
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

// agentModeCommand builds `ward agent <mode>` with its work, headless, task, and
// reply children.
func agentModeCommand(m containerMode) *cli.Command {
	return &cli.Command{
		Name:  string(m),
		Usage: fmt.Sprintf("Drive %s against a Forgejo issue in an ephemeral container.", m),
		Commands: []*cli.Command{
			agentSurfaceCommand(m, "work", false),
			agentSurfaceCommand(m, "headless", true),
			agentTaskCommand(m),
			agentReplyCommand(m),
			agentAskCommand(m),
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
		&cli.StringFlag{Name: "details", Usage: "extra operator instructions woven into the seeded prompt + pre-flight read (overrides the issue text on conflict)"},
		&cli.StringFlag{Name: "image", Value: containerImageDefault, Usage: "dev-base image to run"},
		&cli.StringFlag{Name: "tag", Value: containerImageTagDefault, Usage: "image tag"},
		&cli.StringFlag{Name: "ward-source", Usage: "mount a local ward checkout and build ward from it instead of downloading the release"},
		&cli.BoolFlag{Name: "aws", Usage: "mount ~/.aws read-only (broad SSM read surface; off by default)"},
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

// resolvedWork bundles resolveAgentWork's output: ref, title, body, comment thread
// (ward#154), the --details note (ward#167), and the seeded prompt.
type resolvedWork struct {
	Ref      agentIssueRef
	Title    string
	Body     string
	Comments []issueComment
	Details  string
	Seed     string
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
	details := strings.TrimSpace(c.String("details"))
	// Fetch comments so the pre-flight sees decisions made there, not just the
	// body (ward#154); degrade to a body-only read on failure (the prior behavior).
	comments, cerr := r.fetchIssueComments(ctx, ref)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "%s: note: could not read comments on %s (%v); pre-flight reads the body only\n", label, ref, cerr)
	}
	return resolvedWork{Ref: ref, Title: title, Body: issue.Body, Comments: comments, Details: details, Seed: agentSeedPrompt(ref, title, issue.Body, details, mode)}, nil
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
	w, err := r.resolveAgentWork(ctx, c, mode, surface)
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
		if perr := r.precheckReservation(ctx, fmt.Sprintf("ward agent %s %s", mode, surface), w, c.Bool("force")); perr != nil {
			return perr
		}
		proceed, perr := r.runPreflight(ctx, mode, surface, w)
		if perr != nil {
			return fmt.Errorf("ward agent %s %s: pre-flight: %w", mode, surface, perr)
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
// GO / NO-GO line, feeding the --details note + comments too (ward#154/#167; see docs).
func preflightPrompt(ref agentIssueRef, title, body, details string, comments []issueComment) string {
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
	return fmt.Sprintf(
		"You are about to be sent, fire-and-forget, into an ephemeral container to carry "+
			"this Forgejo issue end to end on your own - implement, commit, merge to main, "+
			"push - with no human watching once you detach.\n\n"+
			"That detached run happens in a FRESH CLONE of %s/%s pulled inside the container. "+
			"The directory you are reading this in right now is unrelated host scratch - it may "+
			"hold a different repo, or none at all. So judge feasibility from the issue text "+
			"alone, never from the local working tree: a file, path, or package that looks "+
			"missing in the current directory tells you nothing about the clone you will actually "+
			"get, so do not conclude the issue is mis-filed just because the local tree lacks it.\n\n"+
			"Before that detached run starts, give a quick PRE-FLIGHT read: based on the issue "+
			"AND its comment thread below, do you think you can carry it to merge unattended? "+
			"Later comments can supersede the original description - the author may have answered "+
			"an open question or picked among options there, so weigh the latest word, not just "+
			"the initial framing.\n\n"+
			"Issue: %s (%q)\n\n%s%s\n\n"+
			"Comment thread (oldest first):\n\n%s\n\n"+
			"Answer in 2-4 sentences naming the main risk or unknown, then a final line of "+
			"exactly one of:\n"+
			"  \"GO\" - you would take it on unattended;\n"+
			"  \"NO-GO: <reason>\" - a human should weigh in first;\n"+
			"  \"WRONG-REPO: owner/repo - <what to file there>\" - the work plainly belongs in a "+
			"different repo than %s/%s. Only say this when the issue text alone makes it obvious - "+
			"do not go digging to decide it, and never from files missing in the current directory. "+
			"ward will blind-file a fresh issue in that repo and launch nothing here.\n"+
			"This is a judgment call, not a commitment - be honest about ambiguity.",
		ref.Owner, ref.Repo, ref, title, body, note, thread, ref.Owner, ref.Repo)
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
	label := fmt.Sprintf("ward agent %s %s", mode, surface)
	bin := mode.agentBinary()
	argv, ok := mode.hostPreflightArgv(preflightPrompt(w.Ref, w.Title, w.Body, w.Details, w.Comments))
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
		return r.handlePreflightWrongRepo(ctx, mode, surface, w, outcome, read)
	case verdictNoGo:
		fmt.Fprintf(os.Stderr, "%s: pre-flight NO-GO for %s; launching nothing, commenting on the issue.\n", label, w.Ref)
		if cerr := r.postPreflightNoGo(ctx, mode, surface, w.Ref, outcome.Reason, read); cerr != nil {
			return false, fmt.Errorf("post NO-GO comment on %s: %w", w.Ref, cerr)
		}
		fmt.Fprintf(os.Stderr, "%s: commented NO-GO on %s - %s\n", label, w.Ref, w.Ref.url())
		return false, nil
	default:
		return true, nil
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
// into a trusted target repo, else bounce to a human. See docs/agent.md.
func (r *Runner) handlePreflightWrongRepo(ctx context.Context, mode containerMode, surface string, w resolvedWork, outcome preflightOutcome, read string) (bool, error) {
	label := fmt.Sprintf("ward agent %s %s", mode, surface)
	target, ok := wrongRepoTarget(outcome.Repo)
	sameRepo := ok && target.Owner == w.Ref.Owner && target.Name == w.Ref.Repo
	// An untrusted repo, the issue's own repo, or a half target is no blind-fire
	// target: bounce to a human rather than guessing.
	if !ok || sameRepo || !r.ownerAllowed(target.Owner) {
		reason := outcome.Reason
		if reason == "" {
			reason = "agent flagged this as belonging in another repo"
		}
		switch {
		case !ok:
			reason = "agent flagged WRONG-REPO but named no usable owner/repo: " + reason
		case sameRepo:
			reason = "agent flagged WRONG-REPO but named this same repo: " + reason
		default:
			reason = fmt.Sprintf("agent routed this to untrusted repo %s (not in %s): %s",
				target.slug(), strings.Join(r.primaryOrgs(), ", "), reason)
		}
		fmt.Fprintf(os.Stderr, "%s: pre-flight WRONG-REPO unusable for %s; bouncing to a human.\n", label, w.Ref)
		if cerr := r.postPreflightNoGo(ctx, mode, surface, w.Ref, reason, read); cerr != nil {
			return false, fmt.Errorf("post NO-GO comment on %s: %w", w.Ref, cerr)
		}
		return false, nil
	}

	fmt.Fprintf(os.Stderr, "%s: pre-flight WRONG-REPO for %s -> %s; blind-firing an issue there, launching nothing.\n", label, w.Ref, target.slug())
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return false, err
	}
	signed := cl.withMode(mode)
	number, err := signed.createIssue(ctx, target.Owner, target.Name,
		w.Title, blindfireIssueBody(mode, surface, w, outcome.Reason))
	if err != nil {
		return false, fmt.Errorf("blind-fire issue into %s: %w", target.slug(), err)
	}
	filed := agentIssueRef{Owner: target.Owner, Repo: target.Name, Number: number}
	fmt.Fprintf(os.Stderr, "%s: blind-fired %s - %s\n", label, filed, filed.url())
	// Point the original issue at the freshly-filed one so the trail is visible.
	if cerr := signed.commentIssue(ctx, w.Ref.Owner, w.Ref.Repo, w.Ref.Number,
		preflightWrongRepoComment(mode, surface, filed, outcome.Reason, read)); cerr != nil {
		return false, fmt.Errorf("comment WRONG-REPO routing on %s: %w", w.Ref, cerr)
	}
	fmt.Fprintf(os.Stderr, "%s: noted the routing on %s - %s\n", label, w.Ref, w.Ref.url())
	return false, nil
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
	fmt.Fprintf(&b, "`ward agent %s %s` ran a pre-flight feasibility read on this issue before "+
		"detaching a fire-and-forget run, and the agent judged it **NO-GO** - it should not be carried "+
		"unattended until a human weighs in.\n\n", mode, surface)
	fmt.Fprintf(&b, "> %s\n\n", reason)
	// Re-dispatch points at `headless` for both surfaces: the issue is already
	// filed, so re-running `task` would file a duplicate.
	fmt.Fprintf(&b, "No container was launched. Review the issue (clarify the scope, resolve the unknown, "+
		"or split it), then re-dispatch - `ward agent %s headless <ref> --no-preflight` skips this gate "+
		"once you've decided it's good to go.\n", mode)
	if read = strings.TrimSpace(read); read != "" {
		fmt.Fprintf(&b, "\n<details><summary>full pre-flight read</summary>\n\n%s\n\n</details>\n", read)
	}
	fmt.Fprintf(&b, "\n---\nPosted automatically by `ward agent %s %s` pre-flight (ward#147, ward#149).\n%s", mode, surface, preflightNoGoMarker)
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
	fmt.Fprintf(&b, "Routed here from %s by `ward agent %s %s` pre-flight (ward#159): the feasibility "+
		"read judged this work belongs in this repo, not %s/%s.\n\n", w.Ref, mode, surface, w.Ref.Owner, w.Ref.Repo)
	fmt.Fprintf(&b, "> %s\n\n", reason)
	fmt.Fprintf(&b, "This was filed blind from the source issue's text - nobody searched this repo first, "+
		"so confirm it fits before working it.\n\n")
	fmt.Fprintf(&b, "---\n### Source issue (%s)\n\n%s\n", w.Ref, body)
	fmt.Fprintf(&b, "\n---\nFiled automatically by `ward agent %s %s` pre-flight (ward#159).", mode, surface)
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
	fmt.Fprintf(&b, "`ward agent %s %s` ran a pre-flight read on this issue and judged the work "+
		"belongs in **%s**, not here. Rather than burn cycles searching, it blind-fired a fresh "+
		"issue there:\n\n", mode, surface, filed.repoSlug())
	fmt.Fprintf(&b, "- %s - %s\n\n", filed, filed.url())
	fmt.Fprintf(&b, "> %s\n\n", reason)
	fmt.Fprintf(&b, "No container was launched here. If the routing is wrong, close %s and re-dispatch "+
		"this issue with `ward agent %s headless <ref> --no-preflight` to skip the gate.\n", filed, mode)
	if read = strings.TrimSpace(read); read != "" {
		fmt.Fprintf(&b, "\n<details><summary>full pre-flight read</summary>\n\n%s\n\n</details>\n", read)
	}
	fmt.Fprintf(&b, "\n---\nPosted automatically by `ward agent %s %s` pre-flight (ward#159).", mode, surface)
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

	// Reserve the issue so another run (file sentinel here, Forgejo marker elsewhere)
	// won't redo it. Detached holds for the container's life; attached releases on return.
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
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, r.resolveAgentCreds(ctx, mode))
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
		// task detaches into the same fire-and-forget headless run, so it gets the
		// same autonomous pre-flight gate (ward#149); --no-preflight skips it.
		&cli.BoolFlag{Name: "no-preflight", Usage: "skip the pre-flight feasibility check and detach immediately"},
		&cli.BoolFlag{Name: "go-bootstrap", Usage: "EXPERIMENTAL (ward#181): after ward installs, delegate to the Go 'ward container bootstrap' instead of the bash entrypoint logic. Requires ward in-container - use --ward-source until the image bakes it."},
	}
	return &cli.Command{
		Name: "task",
		Usage: "File the issue first, then carry it: a freeform '<task>' auto-routes to the right repo (ROUTE); " +
			"an explicit owner/repo with --instructions files there directly (DIRECT).",
		ArgsUsage: "['<task>' to auto-route | owner/repo with --instructions]",
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

// runAgentTask routes the task surface (ward#164) to ROUTE or DIRECT mode by
// classifying the positional + flags. See docs/agent-task.md.
func (r *Runner) runAgentTask(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := fmt.Sprintf("ward agent %s task", mode)
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
	label := fmt.Sprintf("ward agent %s task", mode)
	repo, _, err := r.resolveTarget(ctx, repoArg)
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

	// task runs the exact headless flow, so it gets the same pre-flight (ward#149):
	// a NO-GO comments on the just-filed issue and launches nothing. See docs.
	if preflightWanted(c) {
		proceed, perr := r.runPreflight(ctx, mode, "task", resolvedWork{Ref: ref, Title: title, Body: body})
		if perr != nil {
			return fmt.Errorf("%s: pre-flight: %w", label, perr)
		}
		if !proceed {
			// runPreflight already reported the NO-GO and posted the issue comment.
			return nil
		}
	}

	// task's full instruction set is the filed body, so no --details note (ward#167);
	// a non-vision harness inlines that body directly (ward#157).
	seed := agentSeedPrompt(ref, title, body, "", mode)
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
	seed := agentSeedPrompt(previewRef, title, body, "", mode)
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
