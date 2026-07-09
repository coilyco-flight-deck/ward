package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_qa.go wires `ward agent qa`, the opt-in QA inspection role. It is read-only.
// It inspects the candidate issue, branch, PR, and checks, then posts a verdict comment.

// qaVerdict is the structured emit the QA run is asked to produce.
type qaVerdict struct {
	Verdict   string   `json:"verdict"`
	Summary   string   `json:"summary"`
	Evidence  []string `json:"evidence,omitempty"`
	Risks     []string `json:"risks,omitempty"`
	NextSteps []string `json:"next_steps,omitempty"`
}

// agentQAFlags is the QA flag set: the ref-mode depth ladder plus the shared
// container launch controls and print/no-pull preview.
func agentQAFlags() []cli.Flag {
	flags := agentHarnessFlags()
	flags = append(flags,
		&cli.StringFlag{
			Name:    "thoroughness",
			Aliases: []string{"depth"},
			Value:   defaultReplyThoroughness,
			Usage:   "ref mode: how hard to inspect: quick|standard|deep (deeper gets a longer timeout)",
		},
		configFlag(),
	)
	flags = append(flags, agentImageFlags()...)
	return append(flags,
		&cli.BoolFlag{Name: "print", Usage: "resolve the inputs + render the prompt + plan and exit; inspect nothing, post nothing, run nothing"},
		&cli.BoolFlag{Name: "no-pull", Usage: "skip the image pull"},
	)
}

// agentQACommand builds `ward agent qa`: a ref inspects the issue and posts a
// structured verdict comment. It is opt-in and never a landing gate itself.
func agentQACommand() *cli.Command {
	return &cli.Command{
		Name:      "qa",
		Usage:     "Inspect a ticket, branch, PR, and checks, then post a structured QA verdict comment; no implementation edits.",
		ArgsUsage: "<owner/repo#N | #N | forgejo-issue-url> [extra framing]",
		Flags:     agentQAFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := surfaceDispatchMode(c)
			if err != nil {
				return fmt.Errorf("ward agent qa: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".qa",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runAgentQA(ctx, cmd, mode)
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runAgentQA fetches the issue + thread, captures the QA verdict, and posts it as
// a durable comment. A failing verdict is still surfaced but does not block.
func (r *Runner) runAgentQA(ctx context.Context, c *cli.Command, mode containerMode) error {
	label := agentCmdline(mode, "qa")

	ref, prompt, level, err := r.validateQAInputs(ctx, c, label)
	if err != nil {
		return err
	}

	issue, err := r.fetchIssueByForge(ctx, label, ref.Forge, mode, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return fmt.Errorf("%s: resolve issue %s: %w", label, ref, err)
	}
	title := strings.TrimSpace(issue.Title)
	comments, cerr := r.fetchIssueComments(ctx, ref)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "%s: note: could not read comments on %s (%v); QA will inspect the issue body only\n", label, ref, cerr)
	}

	prompt = qaInspectionPrompt(prompt)
	research := qaResearchPrompt(ref, title, issue.Body, comments, prompt, level)

	if c.Bool("print") {
		return printAgentQAPlan(c, mode, ref, title, prompt, level, research)
	}

	r.maybeWarnWardOutdated(ctx)

	read, err := r.captureQAResearch(ctx, c, mode, ref, level, research)
	if err != nil {
		return fmt.Errorf("%s: QA inspection %s: %w", label, ref, err)
	}
	if strings.TrimSpace(read) == "" {
		read = `{"verdict":"blocked","summary":"QA returned no output","evidence":["The container produced an empty read."],"risks":["The inspection did not complete."],"next_steps":["Re-run QA and inspect the container logs."]}`
	}

	cl, err := r.hostForgeClient(ctx, ref.Forge, mode)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, qaVerdictComment(mode, level, prompt, read)); err != nil {
		return fmt.Errorf("%s: post QA verdict on %s: %w", label, ref, err)
	}
	fmt.Fprintf(os.Stderr, "%s: posted a QA verdict on %s - %s\n", label, ref, ref.url())
	return nil
}

// validateQAInputs parses the QA argv: a valid issue ref, optional framing, a
// known thoroughness, and a trusted owner.
func (r *Runner) validateQAInputs(ctx context.Context, c *cli.Command, label string) (agentIssueRef, string, qaThoroughness, error) {
	ref, err := r.resolveAgentIssueRef(ctx, c.Args().First())
	if err != nil {
		return agentIssueRef{}, "", qaThoroughness{}, fmt.Errorf("%s: %w", label, err)
	}
	prompt := strings.TrimSpace(strings.Join(c.Args().Tail(), " "))

	level, err := parseReplyThoroughness(c.String("thoroughness"))
	if err != nil {
		return agentIssueRef{}, "", qaThoroughness{}, fmt.Errorf("%s: %w", label, err)
	}
	if !r.ownerAllowed(ref.Owner) {
		return agentIssueRef{}, "", qaThoroughness{}, r.untrustedOwnerErr(label, ref.Owner)
	}
	return ref, prompt, level, nil
}

type qaThoroughness = replyThoroughness

// qaInspectionPrompt gives ref-mode QA runs their default brief from the issue
// itself, while still letting a caller append extra framing when needed.
func qaInspectionPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	base := "Read the issue title, body, and comment thread below as the QA brief. Inspect the candidate " +
		"branch, any linked pull request, and the available checks in the live repository state. Return a " +
		"structured QA verdict that a human can read at a glance. Do not edit files, commit, push, or " +
		"otherwise change implementation state."
	if prompt == "" {
		return base
	}
	return base + "\n\nAdditional framing from the dispatcher:\n" + prompt
}

// qaResearchPlan recasts a base plan into the read-only, attached, no-TTY one-shot
// the QA capture needs.
func qaResearchPlan(plan upPlan, ref agentIssueRef) upPlan {
	plan.Ask = true
	plan.ReadOnly = true
	plan.Interactive = true
	plan.TTY = false
	plan.Capture = true
	plan.Forge = ref.Forge
	plan.Role = roleQA
	plan.Issue = 0
	plan.Branch = ""
	plan.Name = containerRoleName(roleQA, plan.Mode, plan.Repo, 0, plan.Machine)
	return plan
}

// captureQAResearch runs the QA inspection in a fresh ephemeral container and
// captures its stdout.
func (r *Runner) captureQAResearch(ctx context.Context, c *cli.Command, mode containerMode, ref agentIssueRef, level qaThoroughness, research string) (string, error) {
	label := agentCmdline(mode, "qa")
	repo := targetRepo{Owner: ref.Owner, Name: ref.Repo}
	cwd := resolveInvokeCWD()

	assetsDir, cleanupAssets, err := writeContainerAssets()
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	defer cleanupAssets()

	plan, err := buildUpPlan(c, repo, mode, roleQA, cwd, assetsDir, []string{research}, false)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	plan = qaResearchPlan(plan, ref)

	if err := r.prelaunchDispatch(ctx, c, plan, label); err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}

	launchCreds := r.resolveLaunchCreds(ctx, &plan, mode)
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), plan.Forge, launchCreds)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	defer cleanupEnv()
	defer func() { _ = r.runDockerSilenced(ctx, true, "rm", "-f", plan.Name) }()

	fmt.Fprintf(os.Stderr, "%s: inspecting %s at %s depth in a fresh container (dig up to %s)...\n\n", label, ref, level.Name, level.Timeout)
	rctx, cancel := context.WithTimeout(ctx, level.Timeout+containerResearchSetupBudget)
	defer cancel()
	out, cerr := r.captureDockerSilenced(rctx, dockerCreateArgv(plan, envFile)...)
	read := strings.TrimSpace(out)
	if read != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", read)
	}
	if cerr != nil {
		return read, cerr
	}
	return read, nil
}

// qaResearchPrompt builds the host one-shot prompt (issue, thread, verdict schema).
func qaResearchPrompt(ref agentIssueRef, title, body string, comments []issueComment, prompt string, level qaThoroughness) string {
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
		"You are doing a one-shot QA inspection on a Forgejo issue. You are NOT implementing anything, "+
			"NOT changing code, and NOT carrying this issue to merge. Your job is to inspect the candidate "+
			"branch, any linked pull request, and the current checks, then report a structured verdict.\n\n"+
			"Emit your answer as a SINGLE fenced ```json block and nothing else outside it, in this shape:\n\n"+
			"```json\n{\n"+
			"  \"verdict\": \"pass|fail|blocked\",\n"+
			"  \"summary\": \"<what you found, in GitHub-flavored markdown>\",\n"+
			"  \"evidence\": [\"<bullet-friendly evidence>\"],\n"+
			"  \"risks\": [\"<optional risk>\"],\n"+
			"  \"next_steps\": [\"<optional follow-up>\"]\n"+
			"}\n"+
			"```\n\n"+
			"Treat pass/fail/blocked as advisory verdicts. A fail still gets surfaced and recorded, but it "+
			"does not gate landing in this first slice.\n\n"+
			"Use the issue text, the comment thread, and the live repository state. Inspect the current branch, "+
			"any linked pull request, and the available checks. The repo is a fresh clone in your working "+
			"directory. The issue thread already carries the operator framing and the current release state.\n\n"+
			"QA depth: %s\n\n"+
			"Issue: %s (%q)\n"+
			"URL: %s\n\n"+
			"----- issue body -----\n%s\n----- end issue body -----\n\n"+
			"Comment thread (oldest first):\n\n%s\n\n"+
			"----- the QA request -----\n%s\n----- end request -----",
		level.Guidance, ref, title, ref.url(), body, thread, prompt)
}

// printAgentQAPlan renders the repo, the QA request, and the docker plan without
// running anything.
func printAgentQAPlan(c *cli.Command, mode containerMode, ref agentIssueRef, title, prompt string, level qaThoroughness, research string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	plan, err := buildUpPlan(c, targetRepo{Owner: ref.Owner, Name: ref.Repo}, mode, roleQA, "", "", []string{research}, false)
	if err != nil {
		return err
	}
	plan = qaResearchPlan(plan, ref)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (print)\n", agentCmdline(mode, "qa"))
	fmt.Fprintf(&b, "qa: agent runs a read-only, captured inspection in a fresh ephemeral container\n")
	fmt.Fprintf(&b, "repo:   %s\n", ref.repoSlug())
	fmt.Fprintf(&b, "name:   %s\n", plan.Name)
	fmt.Fprintf(&b, "depth:  %s\n", level.Name)
	fmt.Fprintf(&b, "----- issue -----\n%s\n----- end -----\n", title)
	fmt.Fprintf(&b, "----- QA request -----\n%s\n----- end -----\n", prompt)
	fmt.Fprintf(&b, "----- seeded prompt -----\n%s\n----- end -----\n", research)
	if c.Bool("no-pull") {
		fmt.Fprintf(&b, "# pull skipped (--no-pull); image: %s\n", plan.Image)
	} else {
		fmt.Fprintf(&b, "docker pull %s\n", plan.Image)
	}
	fmt.Fprintf(&b, "docker %s\n", strings.Join(dockerCreateArgv(plan, "<ward-forgejo-token-envfile>"), " "))
	_, err = io.WriteString(out, b.String())
	return err
}

// qaVerdictComment renders the durable tracker comment. A failure verdict is still
// recorded and surfaced, but it never blocks the run.
func qaVerdictComment(mode containerMode, level qaThoroughness, prompt, read string) string {
	if verdict, ok := parseQAVerdict(read); ok {
		return qaVerdictCommentFrom(mode, level, prompt, verdict)
	}
	visible := "WARD-QA: failed ❌"
	return collapsedIssueComment(visible, "qa details", "Could not parse a structured QA verdict.\n\n"+strings.TrimSpace(read))
}

func qaVerdictCommentFrom(_ containerMode, _ qaThoroughness, prompt string, verdict qaVerdict) string {
	status, emoji := qaOutcomeStatus(verdict.Verdict)
	visible := fmt.Sprintf("WARD-QA: %s %s", status, emoji)
	var b strings.Builder
	if s := strings.TrimSpace(verdict.Summary); s != "" {
		fmt.Fprintf(&b, "summary: %s\n\n", s)
	}
	if len(verdict.Evidence) > 0 {
		fmt.Fprintf(&b, "evidence:\n")
		for _, e := range verdict.Evidence {
			if e = strings.TrimSpace(e); e != "" {
				fmt.Fprintf(&b, "- %s\n", e)
			}
		}
		fmt.Fprintln(&b)
	}
	if len(verdict.Risks) > 0 {
		fmt.Fprintf(&b, "risks:\n")
		for _, e := range verdict.Risks {
			if e = strings.TrimSpace(e); e != "" {
				fmt.Fprintf(&b, "- %s\n", e)
			}
		}
		fmt.Fprintln(&b)
	}
	if len(verdict.NextSteps) > 0 {
		fmt.Fprintf(&b, "next steps:\n")
		for _, e := range verdict.NextSteps {
			if e = strings.TrimSpace(e); e != "" {
				fmt.Fprintf(&b, "- %s\n", e)
			}
		}
		fmt.Fprintln(&b)
	}
	if p := strings.TrimSpace(prompt); p != "" {
		fmt.Fprintf(&b, "dispatcher framing:\n%s\n", p)
	}
	return collapsedIssueComment(visible, "qa details", strings.TrimSpace(b.String()))
}

// parseQAVerdict recovers the structured verdict from the read, if present.
func parseQAVerdict(read string) (qaVerdict, bool) {
	blk, ok := extractJSONBlock(read)
	if !ok {
		return qaVerdict{}, false
	}
	var v qaVerdict
	if err := json.Unmarshal([]byte(blk), &v); err != nil {
		return qaVerdict{}, false
	}
	return v, true
}

// qaOutcomeStatus maps the structured verdict to the tracker-visible status line.
func qaOutcomeStatus(verdict string) (status, emoji string) {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "pass", "passed", "ok", "approve", "approved":
		return "done", "✅"
	case "fail", "failed", "reject", "rejected":
		return "failed", "❌"
	default:
		return "blocked", "🛑"
	}
}
