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

const qaFamilyInternal = "internal"

// agentQAFlags is the QA flag set: the ref-mode depth ladder plus the shared
// container launch controls and print/no-pull preview.
func agentQAFlags() []cli.Flag {
	flags := agentHarnessFlags()
	flags = append(flags,
		&cli.StringFlag{
			Name:    "thoroughness",
			Aliases: []string{"depth"},
			Value:   defaultQAThoroughness,
			Usage:   "ref mode: how hard to inspect: quick|standard|deep (deeper gets a longer timeout)",
		},
		configFlag(),
		&cli.StringFlag{Name: "family", Value: qaFamilyInternal, Usage: "QA reviewer family to launch: internal"},
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
		ArgsUsage: "<owner/repo#N | #N | issue-url> [extra framing]",
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

	ref, prompt, level, family, err := r.validateQAInputs(ctx, c, label)
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

	pr, foundPR, prErr := r.findLinkedPullRequest(ctx, ref, issue, comments)
	qaCtx := qaLaunchContext{
		IssueRef:       ref.String(),
		ReviewerFamily: family,
		Workflow:       workflowMachineToken(workflowPullRequestAndMerge),
		RunIdentity:    reviewSessionID(),
	}
	if prErr != nil {
		fmt.Fprintf(os.Stderr, "%s: note: could not resolve linked PR for %s (%v); QA will comment without a reviewed SHA\n", label, ref, prErr)
	} else if foundPR && pr != nil {
		qaCtx.PRRef = pr.Ref(ref.Owner, ref.Repo)
		qaCtx.ReviewedSHA = pr.HeadSHA()
	}
	prompt = qaInspectionPrompt(prompt)
	research := qaResearchPrompt(ref, title, issue.Body, comments, prompt, level, qaCtx)
	research += agentRunBudgetNote(roleQA)

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

	cl, err := r.hostTrackerClient(ctx, ref.trackerOrDefault(), mode)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := cl.CommentIssue(ctx, ref.Owner, ref.Repo, ref.Number, qaVerdictComment(mode, level, family, prompt, qaCtx, read)); err != nil {
		return fmt.Errorf("%s: post QA verdict on %s: %w", label, ref, err)
	}
	fmt.Fprintf(os.Stderr, "%s: posted a QA verdict on %s - %s\n", label, ref, ref.url())
	return nil
}

// validateQAInputs parses the QA argv: a valid issue ref, optional framing, a
// known thoroughness, and a trusted owner.
func (r *Runner) validateQAInputs(ctx context.Context, c *cli.Command, label string) (agentIssueRef, string, qaThoroughness, string, error) {
	ref, err := r.resolveAgentIssueRef(ctx, c.Args().First())
	if err != nil {
		return agentIssueRef{}, "", qaThoroughness{}, "", fmt.Errorf("%s: %w", label, err)
	}
	prompt := strings.TrimSpace(strings.Join(c.Args().Tail(), " "))

	level, err := parseQAThoroughness(c.String("thoroughness"))
	if err != nil {
		return agentIssueRef{}, "", qaThoroughness{}, "", fmt.Errorf("%s: %w", label, err)
	}
	if !r.ownerAllowed(ref.Owner) {
		return agentIssueRef{}, "", qaThoroughness{}, "", r.untrustedOwnerErr(label, ref.Owner)
	}
	family := strings.TrimSpace(c.String("family"))
	if family == "" {
		family = qaFamilyInternal
	}
	if family != qaFamilyInternal {
		return agentIssueRef{}, "", qaThoroughness{}, "", fmt.Errorf("%s: invalid QA family %q: want %s", label, family, qaFamilyInternal)
	}
	return ref, prompt, level, family, nil
}

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

	assetsDir, cleanupAssets, err := writeContainerAssets(ctx, r, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	defer cleanupAssets()

	plan, err := buildUpPlan(c, repo, mode, roleQA, cwd, assetsDir, []string{research}, false)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	plan = qaResearchPlan(plan, ref)
	plan.DispatchRequestID = strings.TrimSpace(os.Getenv(envDispatchRequestID))

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
	rctx, cancel := context.WithTimeout(ctx, level.Timeout+containerInspectionSetupTime)
	defer cancel()
	if inContainer() {
		return r.captureQAResearchFromBroker(rctx, plan, envFile, label)
	}
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

func (r *Runner) captureQAResearchFromBroker(ctx context.Context, plan upPlan, envFile, label string) (string, error) {
	plan.Capture = false
	plan.Interactive = false
	if err := r.createDetachedViaCopy(ctx, plan, envFile); err != nil {
		return "", err
	}
	exit, waitErr := r.captureDockerSilenced(ctx, "wait", plan.Name)
	out, logsErr := r.captureDockerSilenced(ctx, "logs", plan.Name)
	read := strings.TrimSpace(out)
	if read != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", read)
	}
	if waitErr != nil {
		return read, waitErr
	}
	if logsErr != nil {
		return read, logsErr
	}
	if strings.TrimSpace(exit) != "0" {
		return read, fmt.Errorf("%s: QA container exited %s", label, strings.TrimSpace(exit))
	}
	return read, nil
}

// qaResearchPrompt builds the host one-shot prompt (issue, thread, verdict schema).
func qaResearchPrompt(ref agentIssueRef, title, body string, comments []issueComment, prompt string, level qaThoroughness, ctx qaLaunchContext) string {
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
		"You are doing a one-shot QA inspection on the authoritative issue thread for this repo. You are NOT implementing anything, "+
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
			"Current PR ref: %s\n"+
			"Current reviewed SHA: %s\n"+
			"Reviewer family: %s\n"+
			"Run identity: %s\n\n"+
			"QA depth: %s\n\n"+
			"Issue: %s (%q)\n"+
			"URL: %s\n\n"+
			"----- issue body -----\n%s\n----- end issue body -----\n\n"+
			"Comment thread (oldest first):\n\n%s\n\n"+
			"----- the QA request -----\n%s\n----- end request -----",
		ctx.PRRef, ctx.ReviewedSHA, ctx.ReviewerFamily, ctx.RunIdentity, level.Guidance, ref, title, ref.url(), body, thread, prompt)
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
func qaVerdictComment(mode containerMode, level qaThoroughness, family, prompt string, ctx qaLaunchContext, read string) string {
	if verdict, ok := parseQAVerdict(read); ok {
		return qaVerdictCommentFrom(mode, level, family, prompt, ctx, verdict)
	}
	visible := workflowQAVisible("failed", outcomeStatusEmoji("failed"))
	return collapsedIssueComment(visible, "qa details", "Could not parse a structured QA verdict.\n\n"+strings.TrimSpace(read))
}

type qaLaunchContext struct {
	IssueRef       string
	PRRef          string
	ReviewedSHA    string
	ReviewerFamily string
	Workflow       string
	RunIdentity    string
}

func qaVerdictCommentFrom(_ containerMode, _ qaThoroughness, family, prompt string, ctx qaLaunchContext, verdict qaVerdict) string {
	status, emoji := qaOutcomeStatus(verdict.Verdict)
	visible := workflowQAVisible(status, emoji)
	var b strings.Builder
	writef(&b, "verdict: %s\n", strings.ToLower(strings.TrimSpace(verdict.Verdict)))
	writef(&b, "reviewed_sha: %s\n", strings.TrimSpace(ctx.ReviewedSHA))
	writef(&b, "reviewer_family: %s\n", strings.TrimSpace(family))
	writef(&b, "workflow: %s\n", workflowMachineToken(workflowMode(strings.TrimSpace(ctx.Workflow))))
	writef(&b, "issue_ref: %s\n", strings.TrimSpace(ctx.IssueRef))
	writef(&b, "pr_ref: %s\n", strings.TrimSpace(ctx.PRRef))
	writef(&b, "reason: %s\n", strings.TrimSpace(verdict.Summary))
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
	writef(&b, "run_identity: %s\n\n", strings.TrimSpace(ctx.RunIdentity))
	if p := strings.TrimSpace(prompt); p != "" {
		fmt.Fprintf(&b, "dispatcher framing:\n%s\n", p)
	}
	return collapsedIssueComment(visible, "qa details", strings.TrimSpace(b.String()))
}

// qaCommentMeta is the machine-readable verdict recovered from an issue comment.
type qaCommentMeta struct {
	Verdict        string
	ReviewedSHA    string
	ReviewerFamily string
	Workflow       string
	IssueRef       string
	PRRef          string
	Reason         string
	RunIdentity    string
}

func qaCommentLine(ln string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), ">*-•# "))
}

func parseQAVerdictComment(body string) (qaCommentMeta, bool) {
	meta := qaCommentMeta{}
	found := false
	for _, ln := range strings.Split(body, "\n") {
		s := qaCommentLine(ln)
		if s == "" {
			continue
		}
		if header, ok := parseWorkflowCommentHeader(s); ok && strings.HasPrefix(header.Variant, "qa-") {
			found = true
			continue
		}
		if qaParseCommentField(&meta, s) {
			continue
		}
	}
	if !found {
		return qaCommentMeta{}, false
	}
	return meta, true
}

func qaParseCommentField(meta *qaCommentMeta, s string) bool {
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "verdict:"):
		meta.Verdict = strings.TrimSpace(s[len("verdict:"):])
	case strings.HasPrefix(lower, "reviewed_sha:"):
		meta.ReviewedSHA = strings.TrimSpace(s[len("reviewed_sha:"):])
	case strings.HasPrefix(lower, "reviewer_family:"):
		meta.ReviewerFamily = strings.TrimSpace(s[len("reviewer_family:"):])
	case strings.HasPrefix(lower, "workflow:"):
		meta.Workflow = strings.TrimSpace(s[len("workflow:"):])
	case strings.HasPrefix(lower, "issue_ref:"):
		meta.IssueRef = strings.TrimSpace(s[len("issue_ref:"):])
	case strings.HasPrefix(lower, "pr_ref:"):
		meta.PRRef = strings.TrimSpace(s[len("pr_ref:"):])
	case strings.HasPrefix(lower, "reason:"):
		meta.Reason = strings.TrimSpace(s[len("reason:"):])
	case strings.HasPrefix(lower, "run_identity:"):
		meta.RunIdentity = strings.TrimSpace(s[len("run_identity:"):])
	default:
		return false
	}
	return true
}

// findLinkedPullRequest resolves the merge-lane PR for the issue, if any, and
// returns its current Forgejo head SHA for commit-bound QA commentary.
func (r *Runner) findLinkedPullRequest(ctx context.Context, ref agentIssueRef, _ any, _ []issueComment) (*forgejoPullRequest, bool, error) {
	cl := r.hostForgejoClient(ctx)
	prs, err := cl.ListOpenPullRequests(ctx, ref.Owner, ref.Repo, 50)
	if err != nil {
		return nil, false, err
	}
	for _, pr := range prs {
		linked, ok := directorLinkedIssueNumber(ref.Owner, ref.Repo, pr.Body)
		if !ok || linked != ref.Number {
			continue
		}
		wf, ok := directorPRWorkflowMarker(pr.Body)
		if !ok || wf != string(workflowPullRequestAndMerge) {
			continue
		}
		full, err := cl.GetPullRequest(ctx, ref.Owner, ref.Repo, pr.Number)
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(full.Head.SHA) == "" {
			return nil, false, fmt.Errorf("forgejo: pull request %s/%s#%d omitted head sha", ref.Owner, ref.Repo, pr.Number)
		}
		return full, true, nil
	}
	return nil, false, nil
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
