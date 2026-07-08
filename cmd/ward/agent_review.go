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

// reviewBlockedError is the sentinel the command returns on a block, so the verb
// wrapper records a reject and exits non-zero - the seed keys off it to NOT land.
type reviewBlockedError struct{ result reviewpanel.PanelResult }

func (e reviewBlockedError) Error() string {
	return fmt.Sprintf("review gate BLOCKED: %d/%d passing (class %s); landing refused",
		e.result.Passes, e.result.Threshold, e.result.Class)
}

// agentReviewCommand builds `ward agent review`, the pre-landing panel gate plus
// its stats reader. It is a maintenance/gate verb, not a startup role.
func agentReviewCommand() *cli.Command {
	return &cli.Command{
		Name:  "review",
		Usage: "Run the in-container adversarial multi-model review panel over this run's diff before it lands (ward#134).",
		Description: `review is the pre-PR quorum gate. Run it INSIDE a dispatch container,
after CI is green and before you open the pull request or merge: it builds the
diff-vs-main, hands it to a heterogeneous panel of reviewer families OTHER than
the worker's (each told to refute it), and blocks the landing unless a class-tiered
quorum of them pass. It fails closed: a reviewer error, timeout, or an empty panel
never reads as approval. Panel verdicts are persisted to a sidecar log beside the
audit trail. See docs/dispatch-review.md.`,
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

	cfg := reviewpanel.Config{
		Worker:     worker,
		Class:      class,
		Issue:      reviewIssueRef(),
		SessionID:  reviewSessionID(),
		Candidates: reviewerCandidates(),
		Prompt:     r.reviewPromptInput(ctx, c, class),
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

	if perr := appendPanelRecord(result); perr != nil {
		// Persistence is best-effort telemetry; a write failure must not turn a
		// blocking gate into a pass, so log loud and keep the verdict.
		fmt.Fprintf(r.Runner.Stderr, "ward agent review: WARNING: could not persist panel result: %v\n", perr)
	}

	r.reportPanel(c, result)
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
	if p := strings.TrimSpace(c.String("ci-log")); p != "" {
		if b, err := os.ReadFile(p); err == nil { //nolint:gosec // operator-supplied CI log path
			in.CIOutput = string(b)
		} else {
			fmt.Fprintf(r.Runner.Stderr, "ward agent review: WARNING: could not read --ci-log %s: %v\n", p, err)
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

// reviewerCandidates is the fixed reviewer pool (ward#134): opencode (qwen) free, codex
// paid, claude never. Models come from the embedded fleet roster.
func reviewerCandidates() []reviewpanel.Reviewer {
	return []reviewpanel.Reviewer{
		{Family: "opencode", Model: reviewerModel("opencode"), Paid: false},
		{Family: "codex", Model: reviewerModel("codex"), Paid: true},
	}
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
		case "codex":
			if p := codexAuthPath(); p != "" && !fileExists(p) {
				return false, "no codex auth at " + p
			}
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

// codexAuthPath is the codex credential file the reviewer needs, or "" when $HOME is
// unresolvable (availability then skips the auth check rather than false-dropping).
func codexAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".codex", "auth.json")
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
// off, plus the advisory PR-body note when degraded.
func (r *Runner) reportPanel(c *cli.Command, res reviewpanel.PanelResult) {
	if c.Bool("json") {
		if b, err := json.MarshalIndent(res, "", "  "); err == nil {
			fmt.Fprintln(r.Runner.Stdout, string(b))
		}
	}
	w := r.Runner.Stderr
	fmt.Fprintf(w, "\n── ward review panel (%s, class %s) ──\n", res.Worker, res.Class)
	for _, rv := range res.Reviewers {
		note := rv.Reason
		if rv.Error != "" {
			note = "ERROR: " + rv.Error
		}
		fmt.Fprintf(w, "  %-10s %-6s (conf %.2f) %s\n", rv.Family, rv.Verdict, rv.Confidence, truncateLine(note, 100))
	}
	switch res.Gate {
	case reviewpanel.GateAdvisory:
		fmt.Fprintf(w, "%s\n", res.Note)
		fmt.Fprintf(w, "PR-BODY-NOTE: %s\n", res.Note)
	case reviewpanel.GateBlock:
		fmt.Fprintf(w, "panel BLOCKED: %d/%d passing - do NOT land this diff.\n", res.Passes, res.Threshold)
	case reviewpanel.GatePass:
		fmt.Fprintf(w, "panel cleared: %d/%d passing.\n", res.Passes, res.Threshold)
	}
	// The machine line the seed greps: pass|block|advisory.
	fmt.Fprintf(r.Runner.Stdout, "WARD-REVIEW: %s - %d/%d passing (class %s)\n", res.Gate, res.Passes, res.Threshold, res.Class)
}

// printReviewPlan is the --print dry run: show the resolved panel + prompt, run nothing.
func (r *Runner) printReviewPlan(cfg reviewpanel.Config) error {
	w := r.Runner.Stdout
	fmt.Fprintf(w, "ward agent review --print\n")
	fmt.Fprintf(w, "  worker (excluded): %s\n", cfg.Worker)
	fmt.Fprintf(w, "  class:             %s\n", cfg.Class)
	fmt.Fprintf(w, "  issue:             %s\n", cfg.Issue)
	fmt.Fprintf(w, "  candidates:\n")
	for _, rv := range cfg.Candidates {
		tier := "free"
		if rv.Paid {
			tier = "paid"
		}
		excluded := ""
		if strings.EqualFold(rv.Family, cfg.Worker) {
			excluded = " [EXCLUDED: worker's own family]"
		}
		fmt.Fprintf(w, "    - %s (%s, %s)%s\n", rv.Family, rv.Model, tier, excluded)
	}
	fmt.Fprintf(w, "\n----- reviewer prompt -----\n%s\n", reviewpanel.RefutePrompt(cfg.Prompt))
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
