package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// agent_reply.go is the advisor role's ref mode (ward#347, was reply; ward#179): a host
// one-shot research pass posted as an issue comment. See docs/agent-advisor.md.

// replyThoroughness is one rung of the reply depth ladder: how hard the host
// one-shot digs, the wall-clock it gets, and the steer woven into its prompt.
type replyThoroughness struct {
	// Name is the canonical level token (quick|standard|deep).
	Name string
	// Timeout caps the host one-shot for this level - a deeper read gets longer.
	Timeout time.Duration
	// Guidance is the depth steer woven into the research prompt.
	Guidance string
}

// replyThoroughnessLevels is the ordered depth ladder. Default is standard; the
// timeouts scale with depth so a deep dive isn't cut off mid-investigation.
var replyThoroughnessLevels = []replyThoroughness{
	{
		Name:    "quick",
		Timeout: 3 * time.Minute,
		Guidance: "Keep this QUICK: a direct, focused answer from the issue text, its thread, and what " +
			"you already know. Don't go spelunking - a few sentences to a short section is right.",
	},
	{
		Name:    "standard",
		Timeout: 8 * time.Minute,
		Guidance: "Investigate at a STANDARD depth: reason it through, pull in the obvious context, and " +
			"give a well-structured answer with the reasoning behind it. Investigate further (e.g. read " +
			"the repo) only where it clearly pays off.",
	},
	{
		Name:    "deep",
		Timeout: 15 * time.Minute,
		Guidance: "Go DEEP: investigate thoroughly. Clone and read the repo if it helps, chase the edge " +
			"cases, weigh alternatives, and cite what you found. Take the time to be comprehensive and " +
			"concrete rather than hand-wavy - this is the exhaustive read.",
	},
}

// defaultReplyThoroughness is the level used when --thoroughness is omitted.
const defaultReplyThoroughness = "standard"

// parseReplyThoroughness resolves a --thoroughness value (case-insensitive) to a
// level, erroring on anything off the ladder so a typo never silently downgrades.
func parseReplyThoroughness(s string) (replyThoroughness, error) {
	want := strings.ToLower(strings.TrimSpace(s))
	if want == "" {
		want = defaultReplyThoroughness
	}
	for _, lvl := range replyThoroughnessLevels {
		if lvl.Name == want {
			return lvl, nil
		}
	}
	names := make([]string, 0, len(replyThoroughnessLevels))
	for _, lvl := range replyThoroughnessLevels {
		names = append(names, lvl.Name)
	}
	return replyThoroughness{}, fmt.Errorf("unknown --thoroughness %q: want %s", s, strings.Join(names, "|"))
}

// runAgentReply fetches the issue + thread, runs the host one-shot research at the chosen
// depth, and posts the result as a comment (advisor ref mode; ward#347, was reply).
func (r *Runner) runAgentReply(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "advisor")

	ref, prompt, level, err := r.validateReplyInputs(ctx, c, mode, label)
	if err != nil {
		return err
	}

	// Fetch the issue (fail fast before any research) and its thread for context.
	issue, err := r.fetchForgejoIssue(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return fmt.Errorf("%s: resolve issue %s: %w", label, ref, err)
	}
	title := strings.TrimSpace(issue.Title)
	comments, cerr := r.fetchIssueComments(ctx, ref)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "%s: note: could not read comments on %s (%v); researching the body only\n", label, ref, cerr)
	}

	research := replyResearchPrompt(ref, title, issue.Body, comments, prompt, level)

	if c.Bool("print") {
		return printAgentReplyPlan(c, mode, ref, title, prompt, level, research)
	}

	// reply is a host one-shot, so this dispatch is the interactive moment - surface
	// a stale-ward reminder before it researches + comments (ward#143).
	r.maybeWarnWardOutdated(ctx)

	read, err := r.captureReplyResearch(ctx, mode, ref, level, research)
	if err != nil {
		return fmt.Errorf("%s: research %s: %w", label, ref, err)
	}
	if strings.TrimSpace(read) == "" {
		return fmt.Errorf("%s: research on %s produced no output; nothing to post", label, ref)
	}

	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	cl = cl.withMode(mode)

	// The research emits a structured plan: parse + 2+ trusted repos fans out;
	// single-repo or an unparseable read stays a comment (ward#424, agent-advisor.md).
	plan, parsed := parseReplyPlan(read)
	if parsed {
		allowed, dropped := r.partitionReplySpecs(plan.Issues)
		if distinctRepoCount(allowed) >= 2 {
			return r.postReplyFanOut(ctx, cl, mode, ref, level, prompt, plan.Summary, allowed, dropped, label)
		}
		body := plan.singleComment()
		if strings.TrimSpace(body) == "" {
			body = read
		}
		return r.postReplySingle(ctx, cl, mode, ref, level, prompt, body, dropped, label)
	}
	return r.postReplySingle(ctx, cl, mode, ref, level, prompt, read, nil, label)
}

// postReplySingle posts the common-case single comment on the source issue; any
// dropped fan-out targets are noted so the omission is never silent (ward#424).
func (r *Runner) postReplySingle(ctx context.Context, cl *forgejoClient, mode containerMode, ref agentIssueRef, level replyThoroughness, prompt, body string, dropped []droppedReplySpec, label string) error {
	if len(dropped) > 0 {
		body = strings.TrimRight(body, "\n") + "\n\n---\n**Note:** ward dropped these fan-out targets before filing:\n\n" + renderDroppedReplySpecs(dropped)
	}
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, replyComment(mode, level, prompt, body)); err != nil {
		return fmt.Errorf("%s: post reply on %s: %w", label, ref, err)
	}
	fmt.Fprintf(os.Stderr, "%s: posted a %s reply on %s - %s\n", label, level.Name, ref, ref.url())
	return nil
}

// postReplyFanOut files one issue per trusted repo (dependency order, each
// cross-linked upstream), then posts one index comment on the source (ward#424).
func (r *Runner) postReplyFanOut(ctx context.Context, cl *forgejoClient, mode containerMode, ref agentIssueRef, level replyThoroughness, prompt, summary string, allowed []resolvedReplySpec, dropped []droppedReplySpec, label string) error {
	var created []createdReplyChild
	var upstream string
	for i, s := range allowed {
		body := replyChildBody(s.Body, ref, i+1, len(allowed), upstream)
		num, cerr := cl.createIssue(ctx, s.Owner, s.Name, s.Title, body)
		if cerr != nil {
			dropped = append(dropped, droppedReplySpec{Repo: s.slug(), Reason: "create failed: " + cerr.Error()})
			fmt.Fprintf(os.Stderr, "%s: could not file child in %s: %v\n", label, s.slug(), cerr)
			continue
		}
		cref := agentIssueRef{Owner: s.Owner, Repo: s.Name, Number: num}
		created = append(created, createdReplyChild{Ref: cref, Title: s.Title})
		upstream = cref.String()
		fmt.Fprintf(os.Stderr, "%s: filed child %s - %s\n", label, cref, cref.url())
	}
	body := fanOutIndexComment(mode, level, prompt, summary, created, dropped)
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, body); err != nil {
		return fmt.Errorf("%s: post fan-out index on %s: %w", label, ref, err)
	}
	fmt.Fprintf(os.Stderr, "%s: filed %d child issue(s), indexed on %s - %s\n", label, len(created), ref, ref.url())
	return nil
}

// validateReplyInputs parses and gates the reply argv: a valid issue ref, a
// non-empty prompt, a known thoroughness, a trusted owner, and a wired mode.
func (r *Runner) validateReplyInputs(ctx context.Context, c *cli.Command, mode containerMode, label string) (agentIssueRef, string, replyThoroughness, error) {
	ref, err := r.resolveAgentIssueRef(ctx, c.Args().First())
	if err != nil {
		return agentIssueRef{}, "", replyThoroughness{}, fmt.Errorf("%s: %w", label, err)
	}
	// Everything after the ref is the reply prompt, joined so an unquoted
	// multi-word prompt still works (the canonical form is one quoted arg).
	prompt := strings.TrimSpace(strings.Join(c.Args().Tail(), " "))
	if prompt == "" {
		return agentIssueRef{}, "", replyThoroughness{}, fmt.Errorf("%s: no reply prompt: pass it after the issue ref, e.g. %s <ref> \"what would it take to...\"", label, label)
	}

	level, err := parseReplyThoroughness(c.String("thoroughness"))
	if err != nil {
		return agentIssueRef{}, "", replyThoroughness{}, fmt.Errorf("%s: %w", label, err)
	}

	// Trust gate: reply writes a comment under ward's bot identity, so only act on
	// an owner in the primary-org set - the same gate work/task apply.
	if !r.ownerAllowed(ref.Owner) {
		return agentIssueRef{}, "", replyThoroughness{}, r.untrustedOwnerErr(label, ref.Owner)
	}

	// reply rides the host self-assessment slot (claude/goose), the same one the
	// pre-flight and route survey use. Modes without one can't run it.
	bin := lookupAgent(mode).Record().Binary
	if _, ok := lookupAgent(mode).PreflightArgv("probe"); !ok {
		return agentIssueRef{}, "", replyThoroughness{}, fmt.Errorf("%s: reply runs a host one-shot, which %s lacks (only claude|goose are wired); use one of those", label, bin)
	}
	if !hostHasBinary(bin) {
		return agentIssueRef{}, "", replyThoroughness{}, fmt.Errorf("%s: reply needs %s on PATH to research; install it or use a mode whose binary is present", label, bin)
	}
	return ref, prompt, level, nil
}

// captureReplyResearch runs the host one-shot research argv in a neutral temp dir
// (never the dispatch cwd; mirrors the pre-flight), bounded by the level timeout.
func (r *Runner) captureReplyResearch(ctx context.Context, mode containerMode, ref agentIssueRef, level replyThoroughness, research string) (string, error) {
	argv, ok := lookupAgent(mode).PreflightArgv(research)
	if !ok {
		// Guarded earlier, but stay honest rather than panic on a nil argv.
		return "", fmt.Errorf("no host one-shot slot for %s", mode)
	}
	fmt.Fprintf(os.Stderr, "%s: researching %s at %s depth (up to %s)...\n\n", agentCmdline(mode, "advisor"), ref, level.Name, level.Timeout)
	rctx, cancel := context.WithTimeout(ctx, level.Timeout)
	defer cancel()
	out, err := r.capturePreflight(rctx, argv)
	read := strings.TrimSpace(string(out))
	if read != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", read)
	}
	if err != nil {
		return read, err
	}
	return read, nil
}

// replyResearchPrompt builds the host one-shot prompt (issue, thread, question,
// depth steer), contracting that stdout is a structured JSON plan (ward#424). Pure.
func replyResearchPrompt(ref agentIssueRef, title, body string, comments []issueComment, prompt string, level replyThoroughness) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "(untitled)"
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "(no description provided)"
	}
	thread := preflightComments(comments)
	if thread == "" {
		thread = "(no comments yet)"
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "(no prompt given)"
	}
	return fmt.Sprintf(
		"You are doing one-shot research on a Forgejo issue. You are NOT implementing anything, NOT "+
			"changing code, and NOT carrying this issue to merge - your entire job is to research the "+
			"question below and answer it well.\n\n"+
			"Emit your answer as a SINGLE fenced ```json block and nothing else outside it, in this shape:\n\n"+
			"```json\n{\n"+
			"  \"summary\": \"<overview of your findings, in GitHub-flavored markdown>\",\n"+
			"  \"issues\": [\n"+
			"    {\"repo\": \"owner/name\", \"title\": \"<issue title>\", \"body\": \"<the issue body, in GFM>\"}\n"+
			"  ]\n}\n```\n\n"+
			"Choose the structure by the SCOPE of the work:\n"+
			"- If the answer belongs on THIS issue (a direct answer, or work confined to a single repo), put "+
			"the full answer in \"summary\" and leave \"issues\" empty. ward posts it as one comment here. Do "+
			"NOT split single-repo work into child issues.\n"+
			"- Only when the work genuinely spans 2+ DISTINCT repos, emit one issue spec per repo. ward then "+
			"files each as its own trackable issue and posts an index comment here linking them.\n\n"+
			"When you fan out: order \"issues\" by dependency (the one that unblocks the others first), and "+
			"have each \"body\" state its upstream/downstream dependency in prose so an engineer picking up "+
			"one issue knows what it waits on. Each \"repo\" must be a real \"owner/name\" slug; ward drops "+
			"any spec naming an untrusted or malformed repo.\n\n"+
			"You are running on a host in a fresh, empty scratch directory - it is NOT a checkout of the "+
			"repo. Work from the issue text and thread below plus what you know. You may investigate "+
			"further if it helps and the depth warrants it (the repo clones from %s, and you can search "+
			"the web), but never assume a local checkout exists.\n\n"+
			"%s\n\n"+
			"Issue: %s (%q)\n"+
			"URL: %s\n\n"+
			"----- issue body -----\n%s\n----- end issue body -----\n\n"+
			"Comment thread (oldest first):\n\n%s\n\n"+
			"----- the question to answer -----\n%s\n----- end question -----",
		targetRepo{Owner: ref.Owner, Name: ref.Repo}.cloneURL(forgejoBaseURL), level.Guidance, ref, title, ref.url(), body, thread, prompt)
}

// replyIssueSpec is one issue the research proposes filing: a target repo slug, a
// title, and a body. ward creates it deterministically, never the agent (ward#424).
type replyIssueSpec struct {
	Repo  string `json:"repo"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// replyPlan is the structured research emit: a top-level summary plus an ordered
// list of per-repo issue specs (empty for the single-comment case).
type replyPlan struct {
	Summary string           `json:"summary"`
	Issues  []replyIssueSpec `json:"issues"`
}

// resolvedReplySpec is a trust-gated, repo-split issue spec ready to file.
type resolvedReplySpec struct {
	Owner, Name string
	Title, Body string
}

// slug renders the owner/name pair.
func (s resolvedReplySpec) slug() string { return s.Owner + "/" + s.Name }

// droppedReplySpec records a spec ward refused to file and why, so the omission
// shows up in the index/single comment rather than vanishing silently.
type droppedReplySpec struct {
	Repo   string
	Reason string
}

// createdReplyChild is a filed child issue - its ref plus title - for the index.
type createdReplyChild struct {
	Ref   agentIssueRef
	Title string
}

// parseReplyPlan decodes the fenced JSON block from the research read; ok is
// false when none is found or it won't decode, so the caller posts the raw read.
func parseReplyPlan(read string) (*replyPlan, bool) {
	blk, ok := extractJSONBlock(read)
	if !ok {
		return nil, false
	}
	var p replyPlan
	if err := json.Unmarshal([]byte(blk), &p); err != nil {
		return nil, false
	}
	return &p, true
}

// extractJSONBlock pulls the JSON out of a read: a ```json fence if present, else
// the outermost brace span. A bad grab fails to decode, so the raw fallback holds.
func extractJSONBlock(read string) (string, bool) {
	if i := strings.Index(read, "```json"); i >= 0 {
		rest := read[i+len("```json"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			return strings.TrimSpace(rest[:j]), true
		}
	}
	if i := strings.Index(read, "{"); i >= 0 {
		if j := strings.LastIndex(read, "}"); j > i {
			return strings.TrimSpace(read[i : j+1]), true
		}
	}
	return "", false
}

// partitionReplySpecs keeps specs with a valid slug, a trusted owner, and a title;
// everything else is dropped with a reason. Fan-out never touches untrusted (ward#424).
func (r *Runner) partitionReplySpecs(specs []replyIssueSpec) (allowed []resolvedReplySpec, dropped []droppedReplySpec) {
	for _, s := range specs {
		owner, name, okSlug := splitRepoSlug(s.Repo)
		if !okSlug {
			dropped = append(dropped, droppedReplySpec{Repo: strings.TrimSpace(s.Repo), Reason: "not a valid owner/name slug"})
			continue
		}
		if !r.ownerAllowed(owner) {
			dropped = append(dropped, droppedReplySpec{Repo: owner + "/" + name, Reason: fmt.Sprintf("untrusted owner %q (allowed: %s)", owner, strings.Join(r.primaryOrgs(), ", "))})
			continue
		}
		if strings.TrimSpace(s.Title) == "" {
			dropped = append(dropped, droppedReplySpec{Repo: owner + "/" + name, Reason: "empty issue title"})
			continue
		}
		allowed = append(allowed, resolvedReplySpec{Owner: owner, Name: name, Title: strings.TrimSpace(s.Title), Body: s.Body})
	}
	return allowed, dropped
}

// splitRepoSlug parses an "owner/name" slug, rejecting anything with a stray
// separator, whitespace, or an issue-number '#' so a spec can't smuggle a ref.
func splitRepoSlug(s string) (owner, name string, ok bool) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(s, " \t#") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// distinctRepoCount counts unique repo slugs across resolved specs - the fan-out
// trigger is 2+ distinct trusted repos (ward#424).
func distinctRepoCount(specs []resolvedReplySpec) int {
	seen := make(map[string]struct{}, len(specs))
	for _, s := range specs {
		seen[s.slug()] = struct{}{}
	}
	return len(seen)
}

// singleComment folds a parsed plan into one comment body (summary + any specs as
// sections) for the single-repo/simple case that didn't clear the fan-out bar. Pure.
func (p *replyPlan) singleComment() string {
	parts := make([]string, 0, len(p.Issues)+1)
	if s := strings.TrimSpace(p.Summary); s != "" {
		parts = append(parts, s)
	}
	for _, is := range p.Issues {
		sec := strings.TrimSpace(is.Body)
		if t := strings.TrimSpace(is.Title); t != "" {
			sec = strings.TrimSpace("### " + t + "\n\n" + sec)
		}
		if sec != "" {
			parts = append(parts, sec)
		}
	}
	return strings.Join(parts, "\n\n")
}

// replyChildBody appends a ward provenance footer naming the source issue, the
// spec's position in the ordered fan-out, and its upstream dependency. Pure.
func replyChildBody(body string, source agentIssueRef, part, total int, upstream string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\n---\n")
	fmt.Fprintf(&b, "Filed by `ward agent advisor` cross-repo fan-out from %s (part %d of %d, ward#424).", source, part, total)
	if upstream != "" {
		fmt.Fprintf(&b, " Upstream dependency: %s.", upstream)
	}
	return b.String()
}

// renderDroppedReplySpecs formats the dropped-target bullets shared by the index
// and single comments. Pure.
func renderDroppedReplySpecs(dropped []droppedReplySpec) string {
	var b strings.Builder
	for _, d := range dropped {
		repo := strings.TrimSpace(d.Repo)
		if repo == "" {
			repo = "(unnamed)"
		}
		fmt.Fprintf(&b, "- `%s` - %s\n", repo, d.Reason)
	}
	return b.String()
}

// fanOutIndexComment renders the index comment on the source issue (question,
// summary, ordered child links, dropped targets); carries replyReplyMarker. Pure.
func fanOutIndexComment(mode containerMode, level replyThoroughness, prompt, summary string, created []createdReplyChild, dropped []droppedReplySpec) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "(no prompt given)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### 🔎 ward agent advisor - cross-repo fan-out\n\n")
	fmt.Fprintf(&b, "`%s` ran a one-shot **%s** research pass on this question:\n\n", agentCmdline(mode, "advisor"), level.Name)
	fmt.Fprintf(&b, "> %s\n\n", strings.ReplaceAll(prompt, "\n", "\n> "))
	if s := strings.TrimSpace(summary); s != "" {
		fmt.Fprintf(&b, "%s\n\n", s)
	}
	fmt.Fprintf(&b, "---\n\n")
	if len(created) > 0 {
		fmt.Fprintf(&b, "The work spans multiple repos, so it was filed as %d tracked issue(s), in dependency order:\n\n", len(created))
		for i, ch := range created {
			fmt.Fprintf(&b, "%d. [%s](%s) - %s\n", i+1, ch.Ref, ch.Ref.url(), oneLine(ch.Title))
		}
		fmt.Fprintf(&b, "\n")
	} else {
		fmt.Fprintf(&b, "No child issues were filed - every target was dropped (see below).\n\n")
	}
	if len(dropped) > 0 {
		fmt.Fprintf(&b, "**Dropped (not filed):**\n\n%s\n", renderDroppedReplySpecs(dropped))
	}
	fmt.Fprintf(&b, "---\nResearched and posted automatically by `%s` (ward#424). "+
		"This is one-shot research, not a carried change - verify before acting on it.\n%s", agentCmdline(mode, "advisor"), replyReplyMarker)
	return b.String()
}

// replyReplyMarker tags every reply comment so a later pre-flight/route read can
// drop ward's own research from the thread it weighs (mirrors the NO-GO marker).
const replyReplyMarker = "<!-- ward-agent-reply -->"

// replyComment wraps the research read in a header (the question + depth) and a
// provenance footer flagging it as one-shot research, not a carried change. Pure.
func replyComment(mode containerMode, level replyThoroughness, prompt, read string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "(no prompt given)"
	}
	read = strings.TrimSpace(read)
	if read == "" {
		read = "(the research produced no output)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### 🔎 ward agent advisor\n\n")
	fmt.Fprintf(&b, "`%s` ran a one-shot **%s** research pass on this question:\n\n", agentCmdline(mode, "advisor"), level.Name)
	fmt.Fprintf(&b, "> %s\n\n", strings.ReplaceAll(prompt, "\n", "\n> "))
	fmt.Fprintf(&b, "---\n\n%s\n\n", read)
	fmt.Fprintf(&b, "---\nResearched and posted automatically by `%s` (ward#179). "+
		"This is one-shot research, not a carried change - verify before acting on it.\n%s", agentCmdline(mode, "advisor"), replyReplyMarker)
	return b.String()
}

// printAgentReplyPlan renders the resolved issue, the chosen depth, and the
// research prompt without researching or posting - the dry-run preview for reply.
func printAgentReplyPlan(c *cli.Command, mode containerMode, ref agentIssueRef, title, prompt string, level replyThoroughness, research string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print, ref mode)\n", agentCmdline(mode, "advisor"))
	fmt.Fprintf(&b, "issue:        %s\n", ref)
	fmt.Fprintf(&b, "url:          %s\n", ref.url())
	fmt.Fprintf(&b, "title:        %s\n", title)
	fmt.Fprintf(&b, "thoroughness: %s (timeout %s)\n", level.Name, level.Timeout)
	fmt.Fprintf(&b, "----- reply prompt -----\n%s\n----- end -----\n", prompt)
	fmt.Fprintf(&b, "----- research prompt (host one-shot; %s -p) -----\n%s\n----- end -----\n", lookupAgent(mode).Record().Binary, research)
	fmt.Fprintf(&b, "# would research host-side, then post a comment on %s - or, if the plan spans 2+ trusted repos, fan out into per-repo issues + an index comment (ward#424)\n", ref)
	_, err := io.WriteString(out, b.String())
	return err
}
