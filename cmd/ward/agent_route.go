package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// agent_route.go adds ROUTE mode to `ward agent task` (ward#164): a
// freeform task with no repo routes to one live-surveyed. See docs/agent-task.md.

const (
	// inboxOwner / inboxRepo is the intake repo every routed task lands in first,
	// capturing the literal ask before it's routed onward.
	inboxOwner = "coilysiren"
	inboxRepo  = "inbox"
)

// routeSurveyTimeout caps the one-shot route survey so a wedged host agent can't
// hold the dispatch hostage after the intake record is already filed.
const routeSurveyTimeout = 3 * time.Minute

// repoCatalogEntry is one routable repo the survey agent picks from: its slug and
// (best-effort) description, fetched live across the primary orgs.
type repoCatalogEntry struct {
	Slug        string
	Description string
}

// classifyTaskInvocation decides ROUTE vs DIRECT from the positional arg and the
// instruction flags (ward#164); see docs/agent-task.md. Contradictions error.
func classifyTaskInvocation(arg, inline, file string) (route bool, repoArg string, err error) {
	arg = strings.TrimSpace(arg)
	inline = strings.TrimSpace(inline)
	file = strings.TrimSpace(file)
	if arg != "" {
		if slug, ok := taskRepoRef(arg); ok {
			// An explicit owner/repo positional (bare ref or issue URL) is DIRECT,
			// normalized to its canonical slug; unchanged behavior. See taskRepoRef.
			return false, slug, nil
		}
		// A freeform positional is the task text (ROUTE); a competing instruction
		// flag is a contradiction.
		if inline != "" || file != "" {
			return false, "", fmt.Errorf("got a freeform task as the positional argument and also --instructions/--instructions-file; pass the task one way, not both")
		}
		return true, "", nil
	}
	// No positional: DIRECT with a cwd-inferred repo iff instructions were given.
	if inline != "" || file != "" {
		return false, "", nil
	}
	return false, "", fmt.Errorf("no task given: pass a freeform task ('ward agent task \"do the thing\"') to auto-route it, or an explicit owner/repo with --instructions")
}

// taskRepoRef coerces a `task` positional to an owner/repo slug ONLY for a bare
// `owner/repo[#N]` or a Forgejo issue URL; else false for ROUTE (ward#234; docs).
func taskRepoRef(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", false
	}
	// owner/repo#N or an issue URL is DIRECT; a bare #N / N (owner/repo empty,
	// ward#282) is not - let it fall through to ROUTE freeform text.
	if ref, err := parseAgentIssueRef(arg); err == nil && ref.Owner != "" && ref.Repo != "" {
		return ref.Owner + "/" + ref.Repo, true
	}
	// A bare owner/repo (no issue number); a scheme or scp host disqualifies it,
	// keeping this a bare ref and never a path-segment lift out of a longer URL.
	if !strings.Contains(arg, "://") && !strings.Contains(arg, "@") && ownerNameRe.MatchString(arg) {
		m := ownerNameRe.FindStringSubmatch(arg)
		return m[1] + "/" + m[2], true
	}
	return "", false
}

// runAgentTaskRoute carries ROUTE mode (ward#164): intake -> survey -> scoped
// child -> close intake -> carry. --print files nothing. See docs/agent-task.md.
func (r *Runner) runAgentTaskRoute(ctx context.Context, c *cli.Command, mode containerMode, taskText string) error {
	label := agentCmdline(mode, "task")
	taskText = strings.TrimSpace(taskText)
	if err := r.routeSurveyPreconditions(mode, taskText, label); err != nil {
		return err
	}

	title := taskTitle(taskText)

	if c.Bool("print") {
		return printAgentTaskRoutePlan(c, mode, taskText, title)
	}

	// ROUTE always detaches the eventual carry, so host dispatch is the last
	// interactive moment - surface a stale-ward reminder before it files (ward#143).
	r.maybeWarnWardOutdated(ctx)

	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	signed := cl.withMode(mode)

	// 1. File the intake record - the literal ask, captured before routing.
	intakeNum, err := signed.createIssue(ctx, inboxOwner, inboxRepo, title, routeIntakeBody(mode, taskText))
	if err != nil {
		return fmt.Errorf("%s: file intake issue in %s/%s: %w", label, inboxOwner, inboxRepo, err)
	}
	intake := agentIssueRef{Owner: inboxOwner, Repo: inboxRepo, Number: intakeNum}
	fmt.Fprintf(os.Stderr, "%s: filed intake %s - %s\n", label, intake, intake.url())

	// 2. Survey the fleet live + route (a one-shot agent call over a live catalog).
	outcome, read, serr := r.surveyRoute(ctx, mode, taskText)
	if serr != nil {
		reason := fmt.Sprintf("route survey did not complete: %v", serr)
		return r.bounceRouteToHuman(ctx, signed, label, mode, intake, reason, read)
	}
	if outcome.Verdict != routeRepo {
		return r.bounceRouteToHuman(ctx, signed, label, mode, intake, routeBounceReason(outcome), read)
	}

	// Validate the routed target: a trusted owner, and never the inbox itself.
	target, reason, ok := r.resolveRouteTarget(outcome)
	if !ok {
		return r.bounceRouteToHuman(ctx, signed, label, mode, intake, reason, read)
	}

	// 3. File the scoped child issue in the routed repo, cross-linked to intake.
	childBody := routeChildBody(mode, taskText, outcome.Note, intake)
	childNum, err := signed.createIssue(ctx, target.Owner, target.Name, title, childBody)
	if err != nil {
		return fmt.Errorf("%s: file child issue in %s: %w", label, target.slug(), err)
	}
	child := agentIssueRef{Owner: target.Owner, Repo: target.Name, Number: childNum}
	fmt.Fprintf(os.Stderr, "%s: routed %s -> child %s - %s\n", label, intake, child, child.url())

	// 4. Cross-link the child onto the intake record, then close the intake.
	if cerr := signed.commentIssue(ctx, intake.Owner, intake.Repo, intake.Number, routeRoutedComment(mode, child, outcome.Note, read)); cerr != nil {
		return fmt.Errorf("%s: cross-link intake %s: %w", label, intake, cerr)
	}
	if cerr := signed.closeIssue(ctx, intake.Owner, intake.Repo, intake.Number); cerr != nil {
		// The child is filed and cross-linked; a failed close is cosmetic, so warn
		// rather than strand the carry.
		fmt.Fprintf(os.Stderr, "%s: note: could not close intake %s (%v); it's cross-linked, close it by hand\n", label, intake, cerr)
	}

	// 5. Carry the child to merge in a headless container. The survey already
	// served as the feasibility gate, so ROUTE skips the separate pre-flight.
	seed := agentSeedPrompt(child, title, childBody, "", mode, true, nil)
	return r.launchAgentContainer(ctx, c, mode, "task", true, child, title, seed)
}

// routeSurveyPreconditions gates ROUTE before it files anything: a non-empty task
// and a mode with a host self-assessment slot + binary (claude/goose). ward#148.
func (r *Runner) routeSurveyPreconditions(mode containerMode, taskText, label string) error {
	if taskText == "" {
		return fmt.Errorf("%s: empty task", label)
	}
	bin := mode.agentBinary()
	if _, ok := mode.hostPreflightArgv("probe"); !ok {
		return fmt.Errorf("%s: route mode surveys repos with a host self-assessment slot, which %s lacks (ward#148); pass an explicit owner/repo with --instructions to file directly", label, bin)
	}
	if !hostHasBinary(bin) {
		return fmt.Errorf("%s: route mode needs %s on PATH to survey repos; pass an explicit owner/repo with --instructions to file directly", label, bin)
	}
	return nil
}

// routeBounceReason renders the human-bounce reason for a non-REPO survey
// verdict: an explicit UNCLEAR note, or the no-clear-verdict fallback.
func routeBounceReason(outcome routeOutcome) string {
	if outcome.Verdict == routeUnknown {
		return "the survey returned no clear REPO/UNCLEAR verdict"
	}
	return outcome.Note
}

// resolveRouteTarget validates the survey's routed repo: a trusted owner that is
// never the inbox itself. On rejection it returns the human-bounce reason and false.
func (r *Runner) resolveRouteTarget(outcome routeOutcome) (targetRepo, string, bool) {
	target, ok := wrongRepoTarget(outcome.Repo)
	if !ok || !r.ownerAllowed(target.Owner) || (target.Owner == inboxOwner && target.Name == inboxRepo) {
		reason := fmt.Sprintf("survey routed to an unusable target %q (it must be a trusted owner - %s - and not the inbox itself)",
			outcome.Repo, strings.Join(r.primaryOrgs(), ", "))
		return targetRepo{}, reason, false
	}
	return target, "", true
}

// bounceRouteToHuman comments the UNCLEAR verdict on the still-open intake record
// and launches nothing - the consult exit when the survey can't route confidently.
func (r *Runner) bounceRouteToHuman(ctx context.Context, signed *forgejoClient, label string, mode containerMode, intake agentIssueRef, reason, read string) error {
	fmt.Fprintf(os.Stderr, "%s: route UNCLEAR for intake %s; launching nothing, leaving it open for a human.\n", label, intake)
	if cerr := signed.commentIssue(ctx, intake.Owner, intake.Repo, intake.Number, routeUnclearComment(mode, reason, read)); cerr != nil {
		return fmt.Errorf("%s: comment UNCLEAR on %s: %w", label, intake, cerr)
	}
	fmt.Fprintf(os.Stderr, "%s: commented UNCLEAR on %s - %s\n", label, intake, intake.url())
	return nil
}

// surveyRoute builds a live repo catalog, asks the mode's host agent to pick the
// routing target, and returns the parsed verdict plus the raw read.
func (r *Runner) surveyRoute(ctx context.Context, mode containerMode, taskText string) (routeOutcome, string, error) {
	catalog, err := r.surveyRepoCatalog(ctx)
	if err != nil {
		return routeOutcome{}, "", err
	}
	if len(catalog) == 0 {
		return routeOutcome{}, "", fmt.Errorf("no candidate repos found across %s", strings.Join(r.primaryOrgs(), ", "))
	}
	argv, ok := mode.hostPreflightArgv(routeSurveyPrompt(taskText, renderRepoCatalog(catalog)))
	if !ok {
		return routeOutcome{}, "", fmt.Errorf("no host self-assessment slot for %s", mode)
	}
	fmt.Fprintf(os.Stderr, "%s: route survey - asking %s to route this task across %d repos...\n\n", agentCmdline(mode, "task"), mode.agentBinary(), len(catalog))
	sctx, cancel := context.WithTimeout(ctx, routeSurveyTimeout)
	defer cancel()
	out, err := r.capturePreflight(sctx, argv)
	read := strings.TrimSpace(string(out))
	if read != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", read)
	}
	if err != nil {
		return routeOutcome{}, read, err
	}
	return parseRouteVerdict(read), read, nil
}

// surveyRepoCatalog lists routable repos across the primary orgs (dropping
// archived/empty + the inbox); a per-owner list failure is skipped, not fatal.
func (r *Runner) surveyRepoCatalog(ctx context.Context) ([]repoCatalogEntry, error) {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return nil, err
	}
	var entries []repoCatalogEntry
	for _, owner := range r.primaryOrgs() {
		repos, err := cl.listOwnerRepos(ctx, owner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ward agent task: note: could not list repos for %s (%v); skipping it in the survey\n", owner, err)
			continue
		}
		for _, rb := range repos {
			if rb.Archived || rb.Empty {
				continue
			}
			if owner == inboxOwner && rb.Name == inboxRepo {
				continue
			}
			entries = append(entries, repoCatalogEntry{Slug: owner + "/" + rb.Name, Description: strings.TrimSpace(rb.Description)})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Slug < entries[j].Slug })
	return entries, nil
}

// renderRepoCatalog formats the candidate repos as one `owner/repo — description`
// line each, for embedding in the survey prompt. Pure + testable.
func renderRepoCatalog(entries []repoCatalogEntry) string {
	var b strings.Builder
	for _, e := range entries {
		desc := strings.TrimSpace(e.Description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- %s — %s\n", e.Slug, desc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// routeSurveyPrompt asks the host agent to route a freeform task to one cataloged
// repo, ending on a REPO / UNCLEAR verdict line ward parses. Pure + testable.
func routeSurveyPrompt(taskText, catalog string) string {
	taskText = strings.TrimSpace(taskText)
	if taskText == "" {
		taskText = "(no task given)"
	}
	return fmt.Sprintf(
		"You are routing a freeform task to the single repository where its work belongs. "+
			"A scoped issue will be filed in the repo you pick and carried to a merged PR by an "+
			"autonomous agent, so picking the wrong repo wastes a full run - only commit when the "+
			"task plainly fits one repo's purpose.\n\n"+
			"TASK (verbatim):\n%s\n\n"+
			"You may route ONLY to a repo from this catalog (one per line, `owner/repo — description`):\n\n%s\n\n"+
			"Decide from the task and the catalog descriptions. If several could fit, or the task is "+
			"too vague to place, say UNCLEAR rather than guessing - a human will route it.\n\n"+
			"Answer in 1-3 sentences naming your choice and why, then a final line of exactly one of:\n"+
			"  \"REPO: owner/repo - <one sentence scoping the task for that repo>\" - you are confident it belongs there;\n"+
			"  \"UNCLEAR: <what is ambiguous or which repos compete>\" - no single repo clearly fits.\n"+
			"The owner/repo on a REPO line MUST be copied exactly from the catalog above.",
		taskText, catalog)
}

// routeVerdict is ward's read of the survey's routing decision.
type routeVerdict int

const (
	routeUnknown routeVerdict = iota // no clear verdict line - bounced to a human
	routeRepo                        // an explicit REPO (owner/repo + scoping note)
	routeUnclear                     // an explicit UNCLEAR (carries a reason)
)

// routeOutcome is ward's parsed read of the verdict line: the verdict, the routed
// owner/repo (REPO only), and the scoping note (REPO) or reason (UNCLEAR).
type routeOutcome struct {
	Verdict routeVerdict
	Repo    string
	Note    string
}

var (
	// routeRepoRE matches a REPO line, capturing the owner/repo then the scoping
	// note; checked before the UNCLEAR form.
	routeRepoRE = regexp.MustCompile(`(?i)^repo\b[\s:.\-–—]*([A-Za-z0-9._-]+/[A-Za-z0-9._-]+)\b[\s:.\-–—]*(.*)$`)
	// routeUnclearRE matches an UNCLEAR verdict line and captures the trailing reason.
	routeUnclearRE = regexp.MustCompile(`(?i)^unclear\b[\s:.\-–—]*(.*)$`)
)

// parseRouteVerdict reads the agent's final REPO / UNCLEAR line, tolerating
// markdown decoration; the last verdict line wins. Mirrors parsePreflightVerdict.
func parseRouteVerdict(read string) routeOutcome {
	out := routeOutcome{Verdict: routeUnknown}
	for _, raw := range strings.Split(read, "\n") {
		s := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "*_`>#-•·"))
		if s == "" {
			continue
		}
		if m := routeRepoRE.FindStringSubmatch(s); m != nil {
			out = routeOutcome{Verdict: routeRepo, Repo: m[1], Note: strings.TrimSpace(m[2])}
			continue
		}
		if m := routeUnclearRE.FindStringSubmatch(s); m != nil {
			out = routeOutcome{Verdict: routeUnclear, Note: strings.TrimSpace(m[1])}
		}
	}
	return out
}

// routeIntakeBody renders the intake record body: the literal task verbatim plus
// a note that ward routes a scoped child onward. Pure + testable.
func routeIntakeBody(mode containerMode, taskText string) string {
	taskText = strings.TrimSpace(taskText)
	if taskText == "" {
		taskText = "(no task given)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Intake record for a freeform task captured by `%s` (ward#164). The literal "+
		"ask is below; ward surveys the fleet's repos and routes a scoped child issue to the correct one, "+
		"then cross-links and closes this record.\n\n", agentCmdline(mode, "task"))
	fmt.Fprintf(&b, "---\n### Task (verbatim)\n\n%s\n", taskText)
	fmt.Fprintf(&b, "\n---\nFiled by `%s` (intake; ward#164).", agentCmdline(mode, "task"))
	return b.String()
}

// routeChildBody renders the scoped child issue: the scoping note, the original
// task verbatim, and a cross-link back to the intake record. Pure + testable.
func routeChildBody(mode containerMode, taskText, scoped string, intake agentIssueRef) string {
	taskText = strings.TrimSpace(taskText)
	if taskText == "" {
		taskText = "(no task given)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Routed here from the intake record %s by `%s` (ward#164).\n\n", intake, agentCmdline(mode, "task"))
	if scoped = strings.TrimSpace(scoped); scoped != "" {
		fmt.Fprintf(&b, "**Scoped for this repo:** %s\n\n", scoped)
	}
	fmt.Fprintf(&b, "---\n### Original task (verbatim)\n\n%s\n", taskText)
	fmt.Fprintf(&b, "\n---\nFiled by `%s` route mode (ward#164); intake record: %s", agentCmdline(mode, "task"), intake.url())
	return b.String()
}

// routeRoutedComment renders the comment left on the intake record once the child
// is filed: where the work was routed, the scoping note, and the survey read. Pure.
func routeRoutedComment(mode containerMode, child agentIssueRef, scoped, read string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### 🧭 ward task route\n\n")
	fmt.Fprintf(&b, "`%s` surveyed the fleet and routed this task to **%s**:\n\n", agentCmdline(mode, "task"), child.repoSlug())
	fmt.Fprintf(&b, "- %s - %s\n\n", child, child.url())
	if scoped = strings.TrimSpace(scoped); scoped != "" {
		fmt.Fprintf(&b, "> %s\n\n", scoped)
	}
	fmt.Fprintf(&b, "Closing this intake record; follow the child issue for the carry-to-merge.\n")
	if read = strings.TrimSpace(read); read != "" {
		fmt.Fprintf(&b, "\n<details><summary>full route survey</summary>\n\n%s\n\n</details>\n", read)
	}
	fmt.Fprintf(&b, "\n---\nPosted automatically by `%s` route (ward#164).", agentCmdline(mode, "task"))
	return b.String()
}

// routeUnclearComment renders the intake comment when the survey can't route:
// the reason and how to re-dispatch by hand. Pure + testable.
func routeUnclearComment(mode containerMode, reason, read string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "(no reason given)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### 🧭 ward task route: UNCLEAR\n\n")
	fmt.Fprintf(&b, "`%s` surveyed the fleet but could not confidently route this task, so it "+
		"filed nothing downstream and launched no container. This intake record stays open for a human to "+
		"route.\n\n", agentCmdline(mode, "task"))
	fmt.Fprintf(&b, "> %s\n\n", reason)
	fmt.Fprintf(&b, "Once you know the target repo, re-dispatch in DIRECT mode - `%s <owner/repo> "+
		"-i \"...\"` - or file the issue by hand and run `%s <ref>`.\n", agentCmdline(mode, "task"), agentCmdline(mode, "headless"))
	if read = strings.TrimSpace(read); read != "" {
		fmt.Fprintf(&b, "\n<details><summary>full route survey</summary>\n\n%s\n\n</details>\n", read)
	}
	fmt.Fprintf(&b, "\n---\nPosted automatically by `%s` route (ward#164).", agentCmdline(mode, "task"))
	return b.String()
}

// printAgentTaskRoutePlan renders the intake record that *would* be filed and the
// downstream route flow, filing nothing and running nothing - ROUTE's dry-run.
func printAgentTaskRoutePlan(c *cli.Command, mode containerMode, taskText, title string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print, route mode)\n", agentCmdline(mode, "task"))
	fmt.Fprintf(&b, "mode:    route (no explicit owner/repo given)\n")
	fmt.Fprintf(&b, "intake:  %s/%s (the literal task is filed here first, then routed live)\n", inboxOwner, inboxRepo)
	fmt.Fprintf(&b, "title:   %s\n", title)
	fmt.Fprintf(&b, "----- intake issue to file -----\ntitle: %s\n\n%s\n----- end -----\n", title, routeIntakeBody(mode, taskText))
	fmt.Fprintf(&b, "# then, live: survey repos across %s, file a scoped child issue in the routed repo,\n", strings.Join(defaultPrimaryOrgs(), ", "))
	fmt.Fprintf(&b, "# cross-link + close the intake record, and carry the child to merge headless.\n")
	fmt.Fprintf(&b, "# an UNCLEAR survey files no child and launches nothing, leaving the intake record for a human.\n")
	_, err := io.WriteString(out, b.String())
	return err
}
