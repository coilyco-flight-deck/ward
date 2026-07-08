package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_director_consult.go is `ward agent director consult` (ward#493): the
// consult-to-headless conversion interview. See docs/director-consult.md.

// consultInterviewTimeout caps the per-repo option-generation one-shot so a wedged host
// agent can't wedge the interview; on timeout it proceeds hand-driven (fail-open).
const consultInterviewTimeout = 3 * time.Minute

// consultBodyLimit caps how much of each body rides the batched option-generation
// prompt, keeping a queue's worth of tickets one affordable one-shot (like triage).
const consultBodyLimit = 1000

// consultCandidate is one open issue in the interview queue: a consult ticket, or an
// untriaged one with no mode label. A headless/interactive ticket never enters.
type consultCandidate struct {
	Num   int
	Title string
	Body  string
	Mode  string // "consult" when labelled, "" when untriaged
	URL   string
}

// consultQuestion is the one-shot's interview material for one ticket: the blocking
// decision, options, a recommendation, its consequence. Any field may be empty.
type consultQuestion struct {
	Num         int
	Decision    string
	Options     []string
	Recommend   string
	Consequence string
}

// consultConfig is the resolved knob set for one `ward agent director consult` run.
type consultConfig struct {
	mode    containerMode
	limit   int
	dryRun  bool
	print   bool
	preview bool // dryRun || print: read + rank the queue, write nothing
}

// consultTally counts the terminal disposition of the interview, the done-condition
// ledger: every consult ticket ends headless / merged / closed / kept, or skipped.
type consultTally struct {
	Headless int
	Merged   int
	Closed   int
	Kept     int
	Skipped  int
}

// consultFlags is the interview's flag set: the heartbeat's scope/limit/harness knobs
// plus the launch-nothing previews. No container flags - it dispatches nothing itself.
func consultFlags() []cli.Flag {
	return append(agentHarnessFlags(),
		&cli.StringFlag{Name: "repo", Usage: "comma-separated scope 'a/b,c/d' (default: director.default-scope from ~/.ward/config.yaml, else the cwd git origin)"},
		&cli.StringSliceFlag{Name: "org", Usage: "expand every repo an org owns into the scope (owner; repeatable), unioned with --repo and de-duped"},
		&cli.IntFlag{Name: "limit", Value: directorLimitDefault(), Usage: "open issues read per repo"},
		&cli.BoolFlag{Name: "dry-run", Usage: "show the consult + untriaged queue that would be interviewed, then exit without asking or writing anything"},
		&cli.BoolFlag{Name: "print", Usage: "alias of --dry-run: resolve the queue and exit; write nothing"},
	)
}

// agentConsultCommand wires `ward agent director consult` (audited, trust-gated via the
// shared director scope path; ward#493). Registered under the director command.
func agentConsultCommand() *cli.Command {
	return &cli.Command{
		Name:      "consult",
		Usage:     "Interview a repo's consult + untriaged queue with a human, recording each ticket's blocking decision and flipping it up to headless (ward#493).",
		ArgsUsage: "(scope via --repo; default: the cwd git origin)",
		Description: `consult runs the consult-to-headless conversion interview: the manual launch-triage
loop, encoded. It reads a repo's consult + untriaged open issues, asks one host one-shot
to extract the single blocking human decision each ticket is stuck on (with a concrete
option set + a recommendation), then walks them with a human at the terminal. For each
ticket the human answers freeform (off-menu answers welcome) and picks a disposition:

  [h]eadless - record the answer as a DECISION comment and flip the ticket consult -> headless (now dispatchable)
  [k]eep     - leave it consult with a recorded reason
  [c]lose    - close it as moot with a reason
  [m]erge    - close it as merged into another issue
  [s]kip     - leave it untouched this pass
  [q]uit     - end the interview

  warded director consult --repo coilyco-flight-deck/ward   # one repo
  warded director consult --dry-run                          # show the queue, ask + write nothing

It is attached/interactive only (a human must answer). --dry-run / --print show the
queue and exit. See docs/director-consult.md.`,
		Flags: consultFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentHarness(c)
			if err != nil {
				return fmt.Errorf("ward agent director consult: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".director.consult",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentConsult(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentConsult resolves + trust-gates the scope (reusing director's scope path),
// then drives the interview.
func (r *Runner) runAgentConsult(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "director consult")
	repos, err := r.resolveDirectorScope(ctx, c, label)
	if err != nil {
		return err
	}
	if err := r.backlogTrustGate(label, repos); err != nil {
		return err
	}
	cfg := consultConfig{
		mode:   mode,
		limit:  c.Int("limit"),
		dryRun: c.Bool("dry-run"),
		print:  c.Bool("print"),
	}
	cfg.preview = cfg.dryRun || cfg.print
	return r.runConsult(ctx, label, repos, cfg)
}

// runConsult drives the interview across the scope: per repo read the queue, generate the
// option sets, then walk them with the human. A preview just prints the queue.
func (r *Runner) runConsult(ctx context.Context, label string, repos []string, cfg consultConfig) error {
	if !cfg.preview && !terminalAttached() {
		return fmt.Errorf("%s: the interview is interactive (a human answers each ticket) but no terminal is attached; run it attached, or use --dry-run to preview the queue", label)
	}
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	cl = cl.withMode(cfg.mode)

	in := bufio.NewReader(r.gateIn())
	var tally consultTally
	for _, repo := range repos {
		stop, rerr := r.consultRepo(ctx, label, repo, cl, cfg, in, &tally)
		if rerr != nil {
			return rerr
		}
		if stop {
			break
		}
	}
	if cfg.preview {
		return nil
	}
	return r.consultPrintSummary(repos, tally)
}

// consultRepo interviews one repo's queue; stop=true means the human quit. A read failure
// on one repo is non-fatal to a multi-repo scope (noted, then the next repo runs).
func (r *Runner) consultRepo(ctx context.Context, label, repo string, cl *forgejoClient, cfg consultConfig, in *bufio.Reader, tally *consultTally) (stop bool, err error) {
	owner, name, _ := strings.Cut(repo, "/")
	issues, lerr := cl.listOpenIssues(ctx, owner, name, cfg.limit)
	if lerr != nil {
		writef(os.Stderr, "%s: note: cannot read %s (%v); skipping it.\n", label, repo, lerr)
		return false, nil
	}
	cands := collectConsultCandidates(issues)
	if cfg.preview {
		return false, r.consultPrintQueue(repo, cands)
	}
	if len(cands) == 0 {
		writef(os.Stderr, "%s: %s has no consult or untriaged tickets to interview.\n", label, repo)
		return false, nil
	}
	writef(os.Stderr, "%s: interviewing %d consult/untriaged ticket(s) in %s...\n", label, len(cands), repo)
	questions := r.consultGenerate(ctx, label, cfg.mode, cands)
	w := r.gateErr()
	for _, cand := range cands {
		disp, quit, cerr := r.consultOne(ctx, w, in, cl, repo, cand, questions[cand.Num])
		if cerr != nil {
			writef(w, "%s: note: %s#%d: %v; leaving it untouched.\n", label, repo, cand.Num, cerr)
			disp = "skipped"
		}
		tally.record(disp)
		if quit {
			writef(w, "%s: ending the interview (%s#%d was the last ticket reviewed).\n", label, repo, cand.Num)
			return true, nil
		}
	}
	return false, nil
}

// record folds one ticket's disposition into the tally.
func (t *consultTally) record(disposition string) {
	switch disposition {
	case "headless":
		t.Headless++
	case "merged":
		t.Merged++
	case "closed":
		t.Closed++
	case "kept":
		t.Kept++
	default:
		t.Skipped++
	}
}

// --- candidate collection --------------------------------------------------

// collectConsultCandidates keeps the queue: every open issue not already headless- or
// interactive-labelled (the consult tickets plus the untriaged ones). By number. Pure.
func collectConsultCandidates(issues []backlogIssue) []consultCandidate {
	var out []consultCandidate
	for _, it := range issues {
		mode := backlogModeOf(it.Labels)
		if mode == "headless" || mode == "interactive" {
			continue
		}
		out = append(out, consultCandidate{
			Num:   it.Number,
			Title: it.Title,
			Body:  it.Body,
			Mode:  mode, // "consult" or "" (untriaged)
			URL:   it.URL,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

// --- the option-generation one-shot ----------------------------------------

// consultGenerate runs the batched option-generation one-shot and parses its material. A
// barred/unwired harness or incomplete read returns an empty map (fail-open).
func (r *Runner) consultGenerate(ctx context.Context, label string, mode containerMode, cands []consultCandidate) map[int]consultQuestion {
	bin := lookupAgent(mode).Record().Binary
	argv, stdin, ok := hostOneShot(mode, consultInterviewPrompt(cands))
	if !ok || !hostHasBinary(bin) {
		writef(os.Stderr, "%s: note: %s option-generation unavailable; interviewing from the raw tickets.\n", label, bin)
		return map[int]consultQuestion{}
	}
	writef(os.Stderr, "%s: asking %s to frame the blocking decision for %d ticket(s)...\n", label, bin, len(cands))
	gctx, cancel := context.WithTimeout(ctx, consultInterviewTimeout)
	defer cancel()
	out, err := r.capturePreflight(gctx, argv, stdin)
	if err != nil {
		writef(os.Stderr, "%s: note: option-generation read did not complete (%v); interviewing from the raw tickets.\n", label, err)
		return map[int]consultQuestion{}
	}
	return parseConsultQuestions(strings.TrimSpace(string(out)))
}

// consultInterviewPrompt renders the batched option-generation one-shot: it encodes both
// interrogation moves (human decision + fact to verify), asks one block per ticket. Pure.
func consultInterviewPrompt(cands []consultCandidate) string {
	var b strings.Builder
	b.WriteString("You are the framing pass of a consult-to-headless conversion interview for an autonomous " +
		"backlog. Each ticket below is stuck as `consult` or untriaged: an agent cannot carry it fire-and-forget " +
		"yet. Your job is NOT to solve it - it is to find the ONE thing blocking it, so a human at the terminal " +
		"can answer in a sentence and the ticket flips up to headless-dispatchable.\n\n")
	b.WriteString("For each ticket, identify the single blocking item. It is usually one of:\n" +
		"- a DECISION only a human holds (a design fork, a product stance, a go/no-go, the intent behind a vague ask);\n" +
		"- a FACT a human might misremember that a quick check would settle (did X actually land? does Y still exist?).\n" +
		"Prefer the smallest, most-answerable framing. Give 2-4 concrete options and the one you'd recommend with its " +
		"consequence. When the block is a fact to verify, say so in the DECISION line and phrase the options as the " +
		"branches that fact decides.\n\n")
	b.WriteString("TICKETS:\n")
	for _, c := range cands {
		body := strings.TrimSpace(backlogTruncate(strings.ReplaceAll(c.Body, "\n", " "), consultBodyLimit))
		writef(&b, "- #%d %q :: %s\n", c.Num, backlogTruncate(c.Title, 120), body)
	}
	b.WriteString("\nFor EACH ticket output exactly one block, in this shape, and nothing else between blocks:\n" +
		"=== #<num> ===\n" +
		"DECISION: <one line: the single blocking decision or fact to verify>\n" +
		"OPTION: <a concrete option>\n" +
		"OPTION: <another; 2-4 total>\n" +
		"RECOMMEND: <the option you'd pick>\n" +
		"CONSEQUENCE: <one line: what taking the recommendation commits to>\n")
	return b.String()
}

var (
	consultFieldRE = regexp.MustCompile(`(?i)^(decision|option|recommend(?:ation)?|consequence[s]?)\s*[:=]\s*(.*)$`)
	consultHeadRE  = regexp.MustCompile(`^[\s#=>*\-` + "`" + `]*#(\d+)\b`)
)

// parseConsultQuestions reads the delimited blocks into per-ticket material by number
// (last wins): a field line attaches to the open block, a header opens a new one. Pure.
func parseConsultQuestions(read string) map[int]consultQuestion {
	out := map[int]consultQuestion{}
	var cur *consultQuestion
	flush := func() {
		if cur != nil {
			out[cur.Num] = *cur
		}
	}
	for _, raw := range strings.Split(read, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// A field line is checked first, so "DECISION: ... about #5 ..." never reads as a
		// header for #5. A field with no open block is dropped (no ticket to attach it to).
		if m := consultFieldRE.FindStringSubmatch(line); m != nil {
			if cur == nil {
				continue
			}
			val := strings.TrimSpace(m[2])
			switch strings.SplitN(strings.ToLower(m[1]), "ation", 2)[0] {
			case "decision":
				cur.Decision = val
			case "option":
				if val != "" {
					cur.Options = append(cur.Options, val)
				}
			case "recommend":
				cur.Recommend = val
			case "consequence", "consequences":
				cur.Consequence = val
			}
			continue
		}
		if m := consultHeadRE.FindStringSubmatch(line); m != nil {
			flush()
			num, _ := strconv.Atoi(m[1])
			cur = &consultQuestion{Num: num}
		}
	}
	flush()
	return out
}

// --- the interactive per-ticket loop ---------------------------------------

// consultOne interviews one ticket: present it, loop the disposition menu, do the forge
// write for the chosen action, and return which one landed. quit=true ends the run.
func (r *Runner) consultOne(ctx context.Context, w io.Writer, in *bufio.Reader, cl *forgejoClient, repo string, cand consultCandidate, q consultQuestion) (disposition string, quit bool, err error) { //nolint:gocognit,gocyclo,cyclop
	owner, name, _ := strings.Cut(repo, "/")
	writef(w, "%s", renderConsultTicket(repo, cand, q))
	for {
		writef(w, "\n%s#%d action - [h]eadless / [k]eep consult / [c]lose / [m]erge / [s]kip / [q]uit: ", repo, cand.Num)
		line, rerr := in.ReadString('\n')
		if rerr != nil && strings.TrimSpace(line) == "" {
			// EOF / closed input with nothing pending ends the interview cleanly.
			return "skipped", true, nil
		}
		action, rest := parseConsultAction(line)
		when := consultNow()
		switch action {
		case consultQuit:
			return "skipped", true, nil
		case consultSkip:
			return "skipped", false, nil
		case consultHeadless:
			answer := r.consultFreeform(w, in, rest, "the decision / answer that unblocks it")
			if answer == "" {
				writef(w, "  (no answer given; leaving #%d as consult for now)\n", cand.Num)
				continue
			}
			return "headless", false, r.consultFlipHeadless(ctx, cl, owner, name, cand, q, answer, when)
		case consultKeep:
			reason := r.consultFreeform(w, in, rest, "why it stays consult")
			if reason == "" {
				continue
			}
			if cerr := cl.commentIssue(ctx, owner, name, cand.Num, consultKeepCommentBody(reason, when)); cerr != nil {
				return "skipped", false, cerr
			}
			writef(w, "  #%d kept consult with a recorded reason.\n", cand.Num)
			return "kept", false, nil
		case consultClose:
			reason := r.consultFreeform(w, in, rest, "why it is moot")
			if reason == "" {
				continue
			}
			return "closed", false, r.consultCloseMoot(ctx, cl, owner, name, cand.Num, reason, when)
		case consultMerge:
			target, note := r.consultMergeTarget(w, in, rest)
			if target == 0 {
				writef(w, "  (no target issue given; leaving #%d untouched)\n", cand.Num)
				continue
			}
			return "merged", false, r.consultMergeInto(ctx, cl, owner, name, cand.Num, target, note, when)
		case consultUnknown:
			writeln(w, "  (unrecognized - answer with one of h/k/c/m/s/q)")
		default:
			writeln(w, "  (unrecognized - answer with one of h/k/c/m/s/q)")
		}
	}
}

// renderConsultTicket formats a ticket: ref + title, the generated decision/options when
// present, else a body excerpt so the human can drive the interview by hand. Pure.
func renderConsultTicket(repo string, cand consultCandidate, q consultQuestion) string {
	var b strings.Builder
	tag := "untriaged"
	if cand.Mode == "consult" {
		tag = "consult"
	}
	writef(&b, "\n────────────────────────────────────────\n%s#%d [%s] %s\n", repo, cand.Num, tag, cand.Title)
	if cand.URL != "" {
		writef(&b, "%s\n", cand.URL)
	}
	if q.Decision != "" || len(q.Options) > 0 {
		if q.Decision != "" {
			writef(&b, "\nBlocking decision: %s\n", q.Decision)
		}
		for i, o := range q.Options {
			writef(&b, "  %d) %s\n", i+1, o)
		}
		if q.Recommend != "" {
			writef(&b, "Recommend: %s\n", q.Recommend)
		}
		if q.Consequence != "" {
			writef(&b, "Consequence: %s\n", q.Consequence)
		}
		return b.String()
	}
	if body := strings.TrimSpace(backlogTruncate(strings.ReplaceAll(cand.Body, "\n", " "), 400)); body != "" {
		writef(&b, "\n%s\n", body)
	}
	return b.String()
}

// consultFreeform returns the disposition's freeform text: the inline remainder if typed,
// else a follow-up prompt read from the terminal. A blank answer returns "" (cancel).
func (r *Runner) consultFreeform(w io.Writer, in *bufio.Reader, inline, prompt string) string {
	if s := strings.TrimSpace(inline); s != "" {
		return s
	}
	writef(w, "  %s (blank cancels): ", prompt)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

// consultMergeTarget resolves the merge target issue number + an optional note from the
// inline remainder or a follow-up prompt; target 0 means none was given (cancel).
func (r *Runner) consultMergeTarget(w io.Writer, in *bufio.Reader, inline string) (int, string) {
	text := strings.TrimSpace(inline)
	if text == "" {
		writef(w, "  merge into which issue? (e.g. \"512 superseded by the rewrite\"; blank cancels): ")
		line, _ := in.ReadString('\n')
		text = strings.TrimSpace(line)
	}
	return parseConsultMergeTarget(text)
}

// parseConsultMergeTarget pulls the first issue number out of a merge answer and returns
// the remaining text as the note; no number yields (0, ""). Pure.
func parseConsultMergeTarget(text string) (int, string) {
	m := regexp.MustCompile(`#?(\d+)`).FindStringSubmatchIndex(text)
	if m == nil {
		return 0, ""
	}
	num, err := strconv.Atoi(text[m[2]:m[3]])
	if err != nil {
		return 0, ""
	}
	note := strings.TrimSpace(text[:m[0]] + text[m[1]:])
	return num, note
}

// --- the forge writes ------------------------------------------------------

// consultFlipHeadless records the decision then flips to headless: comment first (so the
// record survives a label failure), then drop consult (if present) and add headless.
func (r *Runner) consultFlipHeadless(ctx context.Context, cl *forgejoClient, owner, name string, cand consultCandidate, q consultQuestion, answer, when string) error {
	if err := cl.commentIssue(ctx, owner, name, cand.Num, consultDecisionCommentBody(q, answer, when)); err != nil {
		return err
	}
	if cand.Mode == "consult" {
		if err := cl.removeIssueLabels(ctx, owner, name, cand.Num, []string{"consult"}); err != nil {
			return fmt.Errorf("decision recorded, but removing the consult label failed: %w", err)
		}
	}
	if err := cl.addIssueLabels(ctx, owner, name, cand.Num, []string{"headless"}); err != nil {
		return fmt.Errorf("decision recorded, but adding the headless label failed: %w", err)
	}
	writef(r.gateErr(), "  #%d flipped to headless with a DECISION comment - now dispatchable.\n", cand.Num)
	return nil
}

// consultCloseMoot comments the moot reason then closes the ticket.
func (r *Runner) consultCloseMoot(ctx context.Context, cl *forgejoClient, owner, name string, num int, reason, when string) error {
	if err := cl.commentIssue(ctx, owner, name, num, consultCloseCommentBody(reason, when)); err != nil {
		return err
	}
	if err := cl.closeIssue(ctx, owner, name, num); err != nil {
		return fmt.Errorf("reason recorded, but closing failed: %w", err)
	}
	writef(r.gateErr(), "  #%d closed as moot.\n", num)
	return nil
}

// consultMergeInto comments the merge target then closes the ticket as superseded.
func (r *Runner) consultMergeInto(ctx context.Context, cl *forgejoClient, owner, name string, num, target int, note, when string) error {
	if err := cl.commentIssue(ctx, owner, name, num, consultMergeCommentBody(target, note, when)); err != nil {
		return err
	}
	if err := cl.closeIssue(ctx, owner, name, num); err != nil {
		return fmt.Errorf("merge note recorded, but closing failed: %w", err)
	}
	writef(r.gateErr(), "  #%d merged into #%d and closed.\n", num, target)
	return nil
}

// --- comment bodies --------------------------------------------------------

// consultDecisionCommentBody renders the DECISION comment for a headless flip: the framed
// decision, the answer, attribution + date (signBody adds the footer). Pure.
func consultDecisionCommentBody(q consultQuestion, answer, when string) string {
	var b strings.Builder
	b.WriteString("## DECISION\n\n")
	if q.Decision != "" {
		writef(&b, "**Blocking decision:** %s\n\n", q.Decision)
	}
	writef(&b, "**Resolved:** %s\n\n", answer)
	writef(&b, "Recorded via `warded director consult` on %s. This ticket is now headless-dispatchable "+
		"(`consult` → `headless`); a fresh agent can carry it from here.\n", when)
	return b.String()
}

// consultKeepCommentBody records why a ticket stays consult. Pure.
func consultKeepCommentBody(reason, when string) string {
	return fmt.Sprintf("## Kept consult\n\nThis ticket stays **consult** for now.\n\n**Reason:** %s\n\n"+
		"Recorded via `warded director consult` on %s.\n", reason, when)
}

// consultCloseCommentBody renders the comment that records a moot close. Pure.
func consultCloseCommentBody(reason, when string) string {
	return fmt.Sprintf("## Closing as moot\n\n**Reason:** %s\n\nClosed via `warded director consult` on %s.\n", reason, when)
}

// consultMergeCommentBody records a merge into another issue. Pure.
func consultMergeCommentBody(target int, note, when string) string {
	var b strings.Builder
	writef(&b, "## Merged into #%d\n\nSuperseded by #%d.", target, target)
	if note = strings.TrimSpace(note); note != "" {
		writef(&b, " %s", note)
	}
	writef(&b, "\n\nMerged and closed via `warded director consult` on %s.\n", when)
	return b.String()
}

// --- disposition parsing ---------------------------------------------------

// consultAction is a disposition the human picks for a ticket in the interview.
type consultAction int

const (
	consultUnknown consultAction = iota
	consultSkip
	consultHeadless
	consultKeep
	consultClose
	consultMerge
	consultQuit
)

// parseConsultAction maps one operator line to a disposition plus the inline remainder
// (the freeform after the action letter). An empty line is a skip. Pure.
func parseConsultAction(line string) (consultAction, string) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return consultSkip, ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
	switch strings.ToLower(fields[0]) {
	case "h", "headless":
		return consultHeadless, rest
	case "k", "keep", "consult":
		return consultKeep, rest
	case "c", "close":
		return consultClose, rest
	case "m", "merge":
		return consultMerge, rest
	case "s", "skip":
		return consultSkip, rest
	case "q", "quit", "exit":
		return consultQuit, rest
	default:
		return consultUnknown, rest
	}
}

// consultNow renders the attribution date stamped into a decision comment.
func consultNow() string { return time.Now().UTC().Format("2006-01-02") }

// --- printing --------------------------------------------------------------

// consultPrintQueue renders the consult + untriaged queue for a --dry-run / --print
// preview, writing nothing to the forge.
func (r *Runner) consultPrintQueue(repo string, cands []consultCandidate) error {
	var b strings.Builder
	writef(&b, "\nconsult queue: %s (%d ticket(s))\n", repo, len(cands))
	for _, c := range cands {
		tag := "untriaged"
		if c.Mode == "consult" {
			tag = "consult"
		}
		writef(&b, "  %s#%-5d [%-9s] %s\n", repo, c.Num, tag, backlogTruncate(c.Title, 70))
	}
	if len(cands) == 0 {
		b.WriteString("  (nothing to interview)\n")
	}
	return r.emit(b.String())
}

// consultPrintSummary prints the interview's terminal disposition tally.
func (r *Runner) consultPrintSummary(repos []string, t consultTally) error {
	var b strings.Builder
	writef(&b, "\nconsult interview summary (%s):\n", strings.Join(repos, ", "))
	writef(&b, "  headless  %d (flipped up, DECISION recorded)\n", t.Headless)
	writef(&b, "  merged    %d\n", t.Merged)
	writef(&b, "  closed    %d\n", t.Closed)
	writef(&b, "  kept      %d (consult, reason recorded)\n", t.Kept)
	writef(&b, "  skipped   %d\n", t.Skipped)
	return r.emit(b.String())
}
