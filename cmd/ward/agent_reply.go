package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_reply.go wires `ward agent reply <issue-ref> <prompt>` (ward#179):
// a host one-shot research pass posted as an issue comment. See docs/agent-reply.md.

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

// agentReplyCommand builds `ward agent reply <issue-ref> <prompt>`: a host one-shot
// research reply as one comment. --driver picks the harness. See docs/agent-reply.md.
func agentReplyCommand() *cli.Command {
	return &cli.Command{
		Name: "reply",
		Usage: "Research an issue one-shot (to a chosen thoroughness) and post the result as an issue comment " +
			"- no container, no code change.",
		ArgsUsage: "<owner/repo#N | forgejo-issue-url> <prompt>",
		Flags: []cli.Flag{
			agentDriverFlag(),
			&cli.StringFlag{
				Name:    "thoroughness",
				Aliases: []string{"depth"},
				Value:   defaultReplyThoroughness,
				Usage:   "how hard to dig: quick|standard|deep (deeper gets a longer timeout)",
			},
			&cli.BoolFlag{Name: "print", Usage: "resolve the issue + render the research prompt and exit; research nothing, post nothing"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentDriver(c)
			if err != nil {
				return fmt.Errorf("ward agent reply: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".reply",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentReply(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentReply fetches the issue + thread, runs the host one-shot research at the
// chosen depth, and posts the result as a comment. Read-only: no container spins.
func (r *Runner) runAgentReply(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "reply")

	ref, prompt, level, err := r.validateReplyInputs(c, mode, label)
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
	if err := cl.withMode(mode).commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, replyComment(mode, level, prompt, read)); err != nil {
		return fmt.Errorf("%s: post reply on %s: %w", label, ref, err)
	}
	fmt.Fprintf(os.Stderr, "%s: posted a %s reply on %s - %s\n", label, level.Name, ref, ref.url())
	return nil
}

// validateReplyInputs parses and gates the reply argv: a valid issue ref, a
// non-empty prompt, a known thoroughness, a trusted owner, and a wired mode.
func (r *Runner) validateReplyInputs(c *cli.Command, mode containerMode, label string) (agentIssueRef, string, replyThoroughness, error) {
	ref, err := parseAgentIssueRef(c.Args().First())
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
		return agentIssueRef{}, "", replyThoroughness{}, fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s)",
			label, ref.Owner, strings.Join(r.primaryOrgs(), ", "))
	}

	// reply rides the host self-assessment slot (claude/goose), the same one the
	// pre-flight and route survey use. Modes without one can't run it.
	bin := mode.agentBinary()
	if _, ok := mode.hostPreflightArgv("probe"); !ok {
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
	argv, ok := mode.hostPreflightArgv(research)
	if !ok {
		// Guarded earlier, but stay honest rather than panic on a nil argv.
		return "", fmt.Errorf("no host one-shot slot for %s", mode)
	}
	fmt.Fprintf(os.Stderr, "%s: researching %s at %s depth (up to %s)...\n\n", agentCmdline(mode, "reply"), ref, level.Name, level.Timeout)
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

// replyResearchPrompt builds the host one-shot prompt: the issue, its thread, the
// question, and the depth steer, contracting that stdout IS the comment. Pure.
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
		"You are doing one-shot research on a Forgejo issue and writing a single comment back to it. "+
			"You are NOT implementing anything, NOT changing code, and NOT carrying this issue to merge - "+
			"your entire job is to research the question below and answer it well.\n\n"+
			"Whatever you print to stdout becomes the issue comment verbatim, so write the answer itself "+
			"in clean GitHub-flavored markdown - no preamble like \"here is my reply\", no sign-off, just "+
			"the content. ward adds its own header and footer.\n\n"+
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
	fmt.Fprintf(&b, "### 🔎 ward agent reply\n\n")
	fmt.Fprintf(&b, "`%s` ran a one-shot **%s** research pass on this question:\n\n", agentCmdline(mode, "reply"), level.Name)
	fmt.Fprintf(&b, "> %s\n\n", strings.ReplaceAll(prompt, "\n", "\n> "))
	fmt.Fprintf(&b, "---\n\n%s\n\n", read)
	fmt.Fprintf(&b, "---\nResearched and posted automatically by `%s` (ward#179). "+
		"This is one-shot research, not a carried change - verify before acting on it.\n%s", agentCmdline(mode, "reply"), replyReplyMarker)
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
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(mode, "reply"))
	fmt.Fprintf(&b, "issue:        %s\n", ref)
	fmt.Fprintf(&b, "url:          %s\n", ref.url())
	fmt.Fprintf(&b, "title:        %s\n", title)
	fmt.Fprintf(&b, "thoroughness: %s (timeout %s)\n", level.Name, level.Timeout)
	fmt.Fprintf(&b, "----- reply prompt -----\n%s\n----- end -----\n", prompt)
	fmt.Fprintf(&b, "----- research prompt (host one-shot; %s -p) -----\n%s\n----- end -----\n", mode.agentBinary(), research)
	fmt.Fprintf(&b, "# would research host-side, then post the result as a comment on %s\n", ref)
	_, err := io.WriteString(out, b.String())
	return err
}
