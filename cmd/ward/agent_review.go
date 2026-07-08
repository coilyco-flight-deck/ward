package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/coilyco-flight-deck/ward/internal/agents/ollamaprobe"
	"github.com/coilyco-flight-deck/ward/internal/reviewpanel"
	"github.com/urfave/cli/v3"
)

// agent_review.go wires `ward agent review` (ward#134): the fleet roster, reviewer
// subprocesses, probes, and sidecar log onto internal/reviewpanel.

// reviewClassEnv pins the panel's autonomy class into the container, so the gate reads
// it from the host, not the (untrusted) worker.
const reviewClassEnv = "WARD_REVIEW_CLASS"

// reviewerTimeout bounds one reviewer subprocess; an overrun fails closed (a hung
// reviewer must never read as a pass).
const reviewerTimeout = 8 * time.Minute

// reviewSkillPath resolves the hand-curated aos code-review skill that the prompt
// embeds into the reviewer context.
func reviewSkillPath() string {
	candidates := []string{"/workspace/agentic-os/.agents/skills/tooling-code-review/SKILL.md"}
	if dest := strings.TrimSpace(os.Getenv("WARD_WORKSPACE_DEST")); dest != "" {
		candidates = append(candidates, filepath.Join(dest, "agentic-os", ".agents", "skills", "tooling-code-review", "SKILL.md"))
	}
	if dest := strings.TrimSpace(os.Getenv("WARD_SUBSTRATE_DEST")); dest != "" {
		candidates = append(candidates, filepath.Join(dest, "agentic-os", ".agents", "skills", "tooling-code-review", "SKILL.md"))
	}
	candidates = append(candidates, filepath.Join(containerSubstrateDest, "agentic-os", ".agents", "skills", "tooling-code-review", "SKILL.md"))
	for _, path := range candidates {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return candidates[len(candidates)-1]
}

// reviewSummaryPath is the handoff file the final conclusion comment reads.
func reviewSummaryPath() (string, error) {
	dir, err := config.GlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "review-summary.txt"), nil
}

// writeReviewSummaryHandoff records the final review summary where the seed asks
// the worker to pick it up for the conclusion comment.
func writeReviewSummaryHandoff(res reviewpanel.PanelResult) error {
	path, err := reviewSummaryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(reviewSummary(res)+"\n"), 0o600) //nolint:gosec // per-run handoff file in ~/.ward
}

// reviewConclusionCommentBody renders the issue comment body that records the
// review conclusion for every gate outcome.
func reviewConclusionCommentBody(res reviewpanel.PanelResult) string {
	var b strings.Builder
	status := "done"
	switch res.Gate {
	case reviewpanel.GateBlock:
		status = "blocked"
	case reviewpanel.GateAdvisory:
		status = "done"
	}
	fmt.Fprintf(&b, "%s %s - review summary: %s\n\n", wardOutcomeMarker, status, reviewSummary(res))
	writef(&b, "Review panel verdicts:\n")
	for _, rv := range res.Reviewers {
		note := rv.Reason
		if rv.Error != "" {
			note = "ERROR: " + rv.Error
		}
		writef(&b, "- %s: %s (conf %.2f)\n", rv.Family, truncateLine(note, 200), rv.Confidence)
	}
	if strings.TrimSpace(res.Note) != "" {
		writef(&b, "\nPanel note: %s\n", truncateLine(res.Note, 200))
	}
	return b.String()
}

// postReviewConclusionComment writes the review conclusion back to the issue thread.
func (r *Runner) postReviewConclusionComment(ctx context.Context, res reviewpanel.PanelResult) {
	ref := strings.TrimSpace(reviewIssueRef())
	if ref == "" {
		return
	}
	parsed, err := parseAgentIssueRef(ref)
	if err != nil {
		writef(r.Runner.Stderr, "ward agent review: WARNING: could not parse issue ref %q for conclusion comment: %v\n", ref, err)
		return
	}
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		writef(r.Runner.Stderr, "ward agent review: WARNING: could not build Forgejo client for conclusion comment: %v\n", err)
		return
	}
	cl = cl.withMode(containerMode(res.Worker))
	if err := cl.commentIssue(ctx, parsed.Owner, parsed.Repo, parsed.Number, reviewConclusionCommentBody(res)); err != nil {
		writef(r.Runner.Stderr, "ward agent review: WARNING: could not post review conclusion comment on %s: %v\n", parsed, err)
	}
}

// reviewSkillFallback is the embedded aos code-review skill text, used when the
// mounted skill file is unavailable.
const reviewSkillFallback = `---
name: tooling-code-review
description: Run a one-shot adversarial code review over a diff. Read the issue contract, inspect the live tree, compare against the intended baseline implementation, and return one fenced JSON verdict. Triggers - code review, review a diff, refute by default, baseline implementation, one-shot review.
---

# Code Review

Use this skill when you must judge a diff, not edit it.

## Why

A good review checks the change against the intended implementation, not just the patch text.

## How to apply

- Read the issue contract first.
- Inspect the live filesystem state and the tests.
- Compare the diff against the baseline implementation the issue expects.
- Assume the diff is wrong until you can prove otherwise.
- Do not edit files, negotiate, or iterate.
- Return exactly one fenced JSON block with verdict, reason, and confidence.
`

// reviewBlockedError is the sentinel the command returns on a block, so the verb
// wrapper records a reject and exits non-zero - the seed keys off it to NOT land.
type reviewBlockedError struct{ result reviewpanel.PanelResult }

func (e reviewBlockedError) Error() string {
	return fmt.Sprintf("review gate BLOCKED: %d/%d passing (class %s); %s; landing refused",
		e.result.Passes, e.result.Threshold, e.result.Class, reviewSummary(e.result))
}

// agentReviewCommand builds `ward agent review`, the pre-landing panel gate plus
// its stats reader. It is a maintenance/gate verb, not a startup role.
func agentReviewCommand() *cli.Command {
	return &cli.Command{
		Name:  "review",
		Usage: "Run the in-container code-review pass over this run's diff before it lands (ward#134).",
		Description: `review is the pre-PR code-review gate. Run it INSIDE a dispatch container,
after CI is green and before you open the pull request or merge: it builds the
diff-vs-main, loads the hand-curated aos review skill, and hands the diff to the
worker's own harness family first, with other families available as a later,
higher-cost fallback. The reviewer runs against the live filesystem state and
blocks the landing unless a class-tiered quorum passes. It fails closed: a reviewer
error, timeout, or an empty panel never reads as approval. Panel verdicts are
persisted to a sidecar log beside the audit trail. See docs/dispatch-review.md.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "class", Sources: cli.EnvVars(reviewClassEnv), Usage: "autonomy/risk class tiering the quorum threshold: lint-cleanup|default|refactor (default default; lint-cleanup=1 pass, default=majority, refactor=unanimous)"},
			&cli.StringFlag{Name: "diff-base", Value: "origin/main", Usage: "git ref the diff-under-review is computed against"},
			&cli.StringFlag{Name: "ci-log", Usage: "path to the green CI/test output to hand the reviewers (optional)"},
			&cli.StringFlag{Name: "worker", Usage: "worker family to exclude from its own panel (default: $WARD_MODE)"},
			&cli.BoolFlag{Name: "print", Usage: "resolve the panel plan + built prompt and exit; run no reviewer"},
			&cli.BoolFlag{Name: "json", Usage: "emit the panel result as JSON on stdout"},
		},
		Action:   agentReviewAction(),
		Commands: []*cli.Command{agentReviewStatsCommand()},
	}
}

// agentReviewAction is the audited gate action.
func agentReviewAction() cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		return r.WrapVerb(verb.Spec{
			Name:       "agent.review",
			SkipPolicy: true,
			ArgsFunc: func(cmd *cli.Command) (map[string]string, []string) {
				return map[string]string{"--class": cmd.String("class")}, nil
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return r.runAgentReview(ctx, cmd)
			},
		}, r.Audit)(ctx, c)
	}
}

// runAgentReview builds the panel config from env + flags, runs the panel, persists +
// prints the result, and returns a blocked error (non-zero exit) when the gate blocks.
func (r *Runner) runAgentReview(ctx context.Context, c *cli.Command) error {
	class, err := reviewpanel.ParseClass(c.String("class"))
	if err != nil {
		return fmt.Errorf("ward agent review: %w", err)
	}
	worker := strings.TrimSpace(c.String("worker"))
	if worker == "" {
		worker = strings.TrimSpace(os.Getenv("WARD_MODE"))
	}
	if worker == "" {
		worker = string(modeClaude)
	}

	prompt := r.reviewPromptInput(ctx, c, class)
	cfg := reviewpanel.Config{
		Worker:     worker,
		Class:      class,
		Issue:      reviewIssueRef(),
		SessionID:  reviewSessionID(),
		Candidates: reviewerCandidates(worker),
		Prompt:     prompt,
	}

	if c.Bool("print") {
		return r.printReviewPlan(cfg)
	}

	deps := reviewpanel.Deps{
		Run:   r.reviewerRunner(ctx),
		Avail: r.reviewerAvailable(ctx),
		Now:   func() int64 { return time.Now().Unix() },
	}
	result := deps.Execute(cfg)

	if result.Gate == reviewpanel.GateAdvisory {
		result.Gate = reviewpanel.GateBlock
		if result.Note == "" {
			result.Note = "review had no runnable reviewer; blocking fail-closed"
		}
	}
	if werr := writeReviewSummaryHandoff(result); werr != nil {
		if path, perr := reviewSummaryPath(); perr == nil {
			writef(r.Runner.Stderr, "ward agent review: WARNING: could not write review summary handoff %s: %v\n", path, werr)
		}
	}
	if perr := appendPanelRecord(result); perr != nil {
		// Persistence is best-effort telemetry; a write failure must not turn a
		// blocking gate into a pass, so log loud and keep the verdict.
		writef(r.Runner.Stderr, "ward agent review: WARNING: could not persist panel result: %v\n", perr)
	}
	r.reportPanel(c, result)
	r.postReviewConclusionComment(ctx, result)
	if result.Blocks() {
		return reviewBlockedError{result: result}
	}
	return nil
}

// reviewPromptInput assembles the reviewer prompt inputs: the diff-vs-base, the
// optional CI log, and the issue contract resolved from the container env.
func (r *Runner) reviewPromptInput(ctx context.Context, c *cli.Command, class reviewpanel.Class) reviewpanel.PromptInput {
	in := reviewpanel.PromptInput{
		Class:    class,
		IssueRef: reviewIssueRef(),
		IssueURL: reviewIssueURL(),
		Diff:     r.reviewDiff(ctx, c.String("diff-base")),
	}
	if ref := strings.TrimSpace(in.IssueRef); ref != "" {
		if parsed, err := parseAgentIssueRef(ref); err == nil {
			if issue, ierr := r.fetchIssue(ctx, parsed); ierr == nil {
				in.IssueTitle = issue.Title
				in.IssueBody = issue.Body
			} else {
				writef(r.Runner.Stderr, "ward agent review: WARNING: could not fetch issue contract for %s: %v\n", ref, ierr)
				in.IssueBody = "(could not fetch issue contract for " + ref + ": " + ierr.Error() + ")"
			}
		}
	}
	if skill, err := os.ReadFile(reviewSkillPath()); err == nil { //nolint:gosec // repo-owned skill path
		in.Skill = string(skill)
	} else {
		in.Skill = reviewSkillFallback
	}
	if p := strings.TrimSpace(c.String("ci-log")); p != "" {
		if b, err := os.ReadFile(p); err == nil { //nolint:gosec // operator-supplied CI log path
			in.CIOutput = string(b)
		} else {
			writef(r.Runner.Stderr, "ward agent review: WARNING: could not read --ci-log %s: %v\n", p, err)
		}
	}
	return in
}

// reviewDiff captures the committed diff of the branch against base; an empty or errored
// diff still runs the panel (the reviewers have the live tree) but is noted.
func (r *Runner) reviewDiff(ctx context.Context, base string) string {
	out, err := r.Runner.Capture(ctx, "git", "diff", base, "HEAD")
	if err != nil {
		return fmt.Sprintf("(could not compute `git diff %s HEAD`: %v - inspect the live tree)", base, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return "(empty diff vs " + base + " - inspect the live tree; an empty diff is itself suspicious)"
	}
	return string(out)
}

// reviewerCandidates is the fixed reviewer pool (ward#134): the worker's own harness
// is the free tier by default, with other harnesses available later as the paid tier.
func reviewerCandidates(worker string) []reviewpanel.Reviewer {
	add := func(out []reviewpanel.Reviewer, family string, paid bool) []reviewpanel.Reviewer {
		return append(out, reviewpanel.Reviewer{Family: family, Model: reviewerModel(family), Paid: paid})
	}
	out := make([]reviewpanel.Reviewer, 0, len(agentModes))
	out = add(out, worker, false)
	for _, mode := range agentModes {
		family := string(mode)
		if strings.EqualFold(family, worker) {
			continue
		}
		out = add(out, family, true)
	}
	return out
}

// reviewerModel reads a family's default model off the effective fleet roster,
// best-effort (an unresolved model is cosmetic, so a load slip yields "").
func reviewerModel(family string) string {
	fleet, err := loadFleetConfig()
	if err != nil {
		return ""
	}
	for _, a := range fleet.Agents {
		if a.Name == family {
			return a.Model
		}
	}
	return ""
}

// reviewerRunner runs one reviewer harness one-shot with the prompt appended and
// captures its stdout, bounded by reviewerTimeout so a hung reviewer fails closed.
func (r *Runner) reviewerRunner(ctx context.Context) reviewpanel.RunFunc {
	return func(rv reviewpanel.Reviewer, prompt string) (string, error) {
		rec := lookupAgent(containerMode(rv.Family)).Record()
		if len(rec.Argv.Headless) == 0 {
			return "", fmt.Errorf("reviewer %s has no headless argv", rv.Family)
		}
		argv := append(append([]string{}, rec.Argv.Headless...), prompt)
		rctx, cancel := context.WithTimeout(ctx, reviewerTimeout)
		defer cancel()
		out, err := r.Runner.Capture(rctx, argv[0], argv[1:]...)
		if err != nil {
			return string(out), fmt.Errorf("%s: %w", rv.Family, err)
		}
		return string(out), nil
	}
}

// reviewerAvailable probes whether a reviewer can run here: its binary must resolve and
// its credential/endpoint be reachable. An unavailable reviewer is dropped.
func (r *Runner) reviewerAvailable(ctx context.Context) reviewpanel.AvailFunc {
	return func(rv reviewpanel.Reviewer) (bool, string) {
		bin := lookupAgent(containerMode(rv.Family)).Record().Binary
		if bin == "" {
			bin = rv.Family
		}
		if _, err := exec.LookPath(bin); err != nil {
			return false, bin + " not on PATH"
		}
		switch rv.Family {
		case "opencode":
			endpoint := strings.TrimSpace(os.Getenv(ollamaprobe.OpencodeEndpointEnv))
			if endpoint == "" {
				return false, "no " + ollamaprobe.OpencodeEndpointEnv + " (ollama endpoint) set"
			}
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := ollamaprobe.ReachOnce(pctx, endpoint); err != nil {
				return false, "ollama unreachable at " + endpoint + ": " + err.Error()
			}
		}
		return true, ""
	}
}

// reviewIssueRef renders owner/repo#N from the container target env, or "".
func reviewIssueRef() string {
	owner, name, num := os.Getenv("WARD_TARGET_OWNER"), os.Getenv("WARD_TARGET_NAME"), os.Getenv("WARD_TARGET_ISSUE")
	if owner == "" || name == "" || num == "" {
		return strings.TrimSpace(os.Getenv("WARD_TARGET_REPO"))
	}
	return fmt.Sprintf("%s/%s#%s", owner, name, num)
}

// reviewIssueURL builds the canonical issue URL from the container target env.
func reviewIssueURL() string {
	owner, name, num := os.Getenv("WARD_TARGET_OWNER"), os.Getenv("WARD_TARGET_NAME"), os.Getenv("WARD_TARGET_ISSUE")
	n, err := strconv.Atoi(num)
	if owner == "" || name == "" || err != nil {
		return ""
	}
	ref := agentIssueRef{Owner: owner, Repo: name, Number: n, Forge: parseForge(os.Getenv("WARD_FORGE"))}
	return ref.url()
}

// reviewSessionID joins the panel row to the harness session that produced it.
func reviewSessionID() string {
	for _, k := range []string{"WARD_CONTAINER_NAME", "CLAUDE_SESSION_ID", "HOSTNAME"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// reportPanel prints the human summary + the machine WARD-REVIEW line the seed keys
// off, plus a concise review summary for the final WARD-OUTCOME comment.
func (r *Runner) reportPanel(c *cli.Command, res reviewpanel.PanelResult) {
	if c.Bool("json") {
		if b, err := json.MarshalIndent(res, "", "  "); err == nil {
			writeln(r.Runner.Stdout, string(b))
		}
	}
	w := r.Runner.Stderr
	writef(w, "\n── ward review panel (%s, class %s) ──\n", res.Worker, res.Class)
	for _, rv := range res.Reviewers {
		note := rv.Reason
		if rv.Error != "" {
			note = "ERROR: " + rv.Error
		}
		writef(w, "  %-10s %-6s (conf %.2f) %s\n", rv.Family, rv.Verdict, rv.Confidence, truncateLine(note, 100))
	}
	if res.Note != "" {
		writef(w, "review note: %s\n", res.Note)
	}
	switch res.Gate {
	case reviewpanel.GateBlock:
		writef(w, "panel BLOCKED: %d/%d passing - do NOT land this diff.\n", res.Passes, res.Threshold)
	case reviewpanel.GatePass:
		writef(w, "panel cleared: %d/%d passing.\n", res.Passes, res.Threshold)
	case reviewpanel.GateAdvisory:
		writef(w, "panel advisory: %d/%d passing - review did not gate this diff.\n", res.Passes, res.Threshold)
	default:
		writef(w, "panel status %q: %d/%d passing.\n", res.Gate, res.Passes, res.Threshold)
	}
	writef(w, "review summary: %s\n", reviewSummary(res))
	// The machine line the seed greps: pass|block|advisory.
	writef(r.Runner.Stdout, "WARD-REVIEW: %s - %d/%d passing (class %s)\n", res.Gate, res.Passes, res.Threshold, res.Class)
}

func reviewSummary(res reviewpanel.PanelResult) string {
	switch res.Gate {
	case reviewpanel.GatePass:
		if len(res.Reviewers) == 0 {
			return "passed"
		}
		last := res.Reviewers[len(res.Reviewers)-1]
		return fmt.Sprintf("passed: %s", truncateLine(last.Reason, 120))
	case reviewpanel.GateAdvisory:
		if strings.TrimSpace(res.Note) != "" {
			return "skipped: " + truncateLine(res.Note, 120)
		}
		return "skipped"
	case reviewpanel.GateBlock:
		if strings.TrimSpace(res.Note) != "" {
			return "blocked: " + truncateLine(res.Note, 120)
		}
		for _, rv := range res.Reviewers {
			if rv.Error != "" {
				return "blocked: " + truncateLine(rv.Error, 120)
			}
			if rv.Verdict == reviewpanel.Block {
				return "blocked: " + truncateLine(rv.Reason, 120)
			}
		}
		if len(res.Reviewers) == 0 {
			return "blocked"
		}
		last := res.Reviewers[len(res.Reviewers)-1]
		if last.Error != "" {
			return "blocked: " + truncateLine(last.Error, 120)
		}
		return "blocked: " + truncateLine(last.Reason, 120)
	default:
		return "unknown"
	}
}

// printReviewPlan is the --print dry run: show the resolved panel + prompt, run nothing.
func (r *Runner) printReviewPlan(cfg reviewpanel.Config) error {
	w := r.Runner.Stdout
	writef(w, "ward agent review --print\n")
	writef(w, "  worker (preferred): %s\n", cfg.Worker)
	writef(w, "  class:             %s\n", cfg.Class)
	writef(w, "  issue:             %s\n", cfg.Issue)
	writef(w, "  candidates:\n")
	for _, rv := range cfg.Candidates {
		tier := "free"
		if rv.Paid {
			tier = "paid"
		}
		note := ""
		if strings.EqualFold(rv.Family, cfg.Worker) {
			note = " [PREFERRED]"
		}
		writef(w, "    - %s (%s, %s)%s\n", rv.Family, rv.Model, tier, note)
	}
	writef(w, "\n----- reviewer prompt -----\n%s\n", reviewpanel.RefutePrompt(cfg.Prompt))
	return nil
}

// panelLogPath resolves the sidecar JSONL beside the audit log: same repo slug, under a
// review-panel/ subdir so the rows sit next to (not inside) the audit trail.
func panelLogPath() (string, error) {
	auditPath, err := config.DefaultAuditPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(auditPath), "review-panel", filepath.Base(auditPath)), nil
}

// appendPanelRecord appends one panel result as a JSONL row to the sidecar log.
func appendPanelRecord(res reviewpanel.PanelResult) error {
	path, err := panelLogPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("panel log dir: %w", err)
	}
	line, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("marshal panel result: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640) //nolint:gosec // config-resolved sidecar path
	if err != nil {
		return fmt.Errorf("open panel log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write panel log: %w", err)
	}
	return nil
}

// truncateLine caps a one-line summary for the panel table.
func truncateLine(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
