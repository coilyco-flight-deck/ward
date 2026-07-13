package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/issueref"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/ownertrust"
	"github.com/coilyco-flight-deck/ward/internal/reviewpanel"
	"github.com/urfave/cli/v3"
)

// agent.go wires the `ward agent` umbrella + the shared dispatch internals the engineer
// role uses (ward#263, ward#347), sharing the bring-up Go directly. See docs/agent.md.

// agentIssueRef is a parsed issue reference for `ward agent`. Forge tags the git
// host, tracker tags the issue thread. The default pairing stays zero-config.
type agentIssueRef struct {
	Owner   string
	Repo    string
	Number  int
	Forge   forge
	Tracker tracker
	// MergeRequest marks the ref as PR/MR-shaped. Forgejo/GitHub treat it as a
	// pull request; GitLab keeps its merge-request wording.
	MergeRequest      bool
	URL               string
	ShortcutWorkspace string
}

func (r agentIssueRef) String() string {
	return fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number)
}

// repoSlug renders the owner/repo pair without the issue number.
func (r agentIssueRef) repoSlug() string {
	return r.Owner + "/" + r.Repo
}

// url renders the canonical issue URL for the seeded prompt.
// Shortcut preserves the story URL it was parsed from.
func (r agentIssueRef) url() string {
	if strings.TrimSpace(r.URL) != "" {
		return strings.TrimSpace(r.URL)
	}
	if r.trackerOrDefault() == trackerShortcut {
		workspace := strings.TrimSpace(r.ShortcutWorkspace)
		if workspace == "" {
			workspace = strings.TrimSpace(os.Getenv(shortcutWorkspaceEnv))
		}
		if workspace != "" {
			return fmt.Sprintf("%s/%s/story/%d", shortcutAppBaseURL, workspace, r.Number)
		}
		return fmt.Sprintf("%s/story/%d", shortcutAppBaseURL, r.Number)
	}
	if r.Forge == forgeGitLab {
		path := "issues"
		if r.MergeRequest {
			path = "merge_requests"
		}
		return fmt.Sprintf("%s/%s/%s/-/%s/%d", strings.TrimRight(r.Forge.baseURL(), "/"), r.Owner, r.Repo, path, r.Number)
	}
	if r.MergeRequest {
		return fmt.Sprintf("%s/%s/%s/%s/%d", strings.TrimRight(r.Forge.baseURL(), "/"), r.Owner, r.Repo, pullRequestPathSegment(r.Forge), r.Number)
	}
	return fmt.Sprintf("%s/%s/%s/issues/%d", strings.TrimRight(r.Forge.baseURL(), "/"), r.Owner, r.Repo, r.Number)
}

func pullRequestPathSegment(f forge) string {
	switch f {
	case forgeForgejo:
		return "pulls"
	case forgeGitHub:
		return "pull"
	case forgeGitLab:
		return "merge_requests"
	}
	return "pulls"
}

// trackerOrDefault resolves the issue-thread port, defaulting to the host's
// paired tracker when the field is left zero.
func (r agentIssueRef) trackerOrDefault() tracker {
	if r.Tracker == trackerGitHub || r.Tracker == trackerForgejo {
		return r.Tracker
	}
	return trackerFromForge(r.Forge)
}

// carryIssueBanner renders the exact carried issue once, as a stable identity
// anchor for prompts and logs. The number is the reserved one, never inferred.
func carryIssueBanner(ref agentIssueRef) string {
	return fmt.Sprintf("Carried issue identity: %s.\nCarried issue number: %d.", ref, ref.Number)
}

// cloneAnchorLine tells the in-container agent it is standing IN the fresh clone
// now - files are its cwd, to read not assume (ward#384; docs/agent-frontload.md).
func cloneAnchorLine(ref agentIssueRef) string {
	return fmt.Sprintf(
		"You are reading this INSIDE that container, standing in a fresh clone of %s/%s at "+
			"/workspace/%s - your current working directory right now: the repo's whole source tree - "+
			"its real schemas, file layouts, and wiring - is on disk right here, not somewhere you have "+
			"to go fetch. In engineer mode that /workspace/%s clone is writable. Explore it directly "+
			"(start with `ls` and the repo's own docs) for any convention this task needs; edit files, "+
			"commit them, and push the result instead of answering only in logs. Never treat the codebase "+
			"as absent or reason from assumed conventions while the actual files sit unread one command away.",
		ref.Owner, ref.Repo, ref.Repo, ref.Repo)
}

// parseAgentIssueRef resolves owner/repo#N, a Forgejo/GitHub issue URL, or a bare #N / N.
// ward keeps the task-verb steer (ward#234, ward#282) while reusing the shared parser.
func parseAgentIssueRef(s string) (agentIssueRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return agentIssueRef{}, fmt.Errorf("empty issue reference")
	}
	if prRef, ok := parseAgentPullRequestRef(s); ok {
		return prRef, nil
	}
	// A github.com URL or `github.com/owner/repo#N` short form is unambiguously a
	// GitHub ref (ward#489). Shortcut story URLs are recognized first as well.
	if ghRef, ok := parseGitHubIssueRef(s); ok {
		return ghRef, nil
	}
	if glRef, ok := parseGitLabIssueRef(s); ok {
		return glRef, nil
	}
	if shortcutRef, ok := parseShortcutIssueRef(s); ok {
		return shortcutRef, nil
	}
	if ref, err := parseDispatchIssueRef(s); err == nil {
		if !looksLikeExplicitForgejoIssueRef(s) {
			ref.Forge = currentSmartDefaults().forgeForRepo(ref.Owner, ref.Repo)
			ref.Tracker = trackerFromForge(ref.Forge)
		}
		return ref, nil
	}
	ref, err := issueref.Parse(s, forgejoBaseURL)
	if err == nil {
		return agentIssueRef{Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number}, nil
	}
	// Accept scheme-less issue URLs as a convenience for dictated refs and
	// pasted URLs that dropped their protocol in transit.
	if !strings.Contains(s, "://") {
		if ref, err := issueref.Parse("https://"+s, forgejoBaseURL); err == nil {
			return agentIssueRef{Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number}, nil
		}
	}
	// A non-issue URL is a valid freeform pointer, just not an issue ref -
	// steer to the task verb that carries arbitrary pointers (ward#234).
	if strings.Contains(s, "://") {
		return agentIssueRef{}, fmt.Errorf(
			"cannot parse issue ref %q: want owner/repo#N, a bare #N, %s/owner/repo/issues/N, %s/owner/repo/-/issues/N, or %s/<workspace>/story/N; "+
				"for a non-issue pointer (a CI run, job, or commit URL), hand it to the engineer "+
				"role's freeform mode instead: ward agent engineer '<url>'",
			s, strings.TrimRight(forgejoBaseURL, "/"), strings.TrimRight(gitlabBaseURL(), "/"), shortcutAppBaseURL)
	}
	return agentIssueRef{}, fmt.Errorf("cannot parse issue ref %q: want owner/repo#N, a bare #N, %s/owner/repo/issues/N, %s/owner/repo/-/issues/N, or %s/<workspace>/story/N", s, strings.TrimRight(forgejoBaseURL, "/"), strings.TrimRight(gitlabBaseURL(), "/"), shortcutAppBaseURL)
}

var forgejoPullRequestRefRE = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?` + regexp.QuoteMeta(strings.TrimRight(forgejoBaseURL, "/")) +
	`/([\w.-]+)/([\w.-]+?)(?:\.git)?/(?:pull|pulls)/(\d+)(?:[/?#].*)?$`)
var githubPullRequestRefRE = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?github\.com/([\w.-]+)/([\w.-]+?)(?:\.git)?/(?:pull|pulls)/(\d+)(?:[/?#].*)?$`)
var compactPullRequestRefRE = regexp.MustCompile(`(?i)^([\w.-]+)/([\w.-]+?)(?:\.git)?!(\d+)$`)

// parseAgentPullRequestRef resolves a PR/MR ref into the shared issue ref shape.
func parseAgentPullRequestRef(s string) (agentIssueRef, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return agentIssueRef{}, false
	}
	if ghRef, ok := parseGitHubPullRequestRef(s); ok {
		return ghRef, true
	}
	if fjRef, ok := parseForgejoPullRequestRef(s); ok {
		return fjRef, true
	}
	if glRef, ok := parseGitLabIssueRef(s); ok && glRef.MergeRequest {
		return glRef, true
	}
	if m := compactPullRequestRefRE.FindStringSubmatch(s); m != nil {
		n, err := parsePositiveInt(m[3])
		if err != nil || n <= 0 {
			return agentIssueRef{}, false
		}
		ref := agentIssueRef{Owner: m[1], Repo: strings.TrimSuffix(m[2], ".git"), Number: n, MergeRequest: true}
		ref.Forge = currentSmartDefaults().forgeForRepo(ref.Owner, ref.Repo)
		ref.Tracker = trackerFromForge(ref.Forge)
		return ref, true
	}
	return agentIssueRef{}, false
}

func parseGitHubPullRequestRef(s string) (agentIssueRef, bool) {
	m := githubPullRequestRefRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return agentIssueRef{}, false
	}
	n, err := parsePositiveInt(m[3])
	if err != nil || n <= 0 {
		return agentIssueRef{}, false
	}
	return agentIssueRef{Owner: m[1], Repo: strings.TrimSuffix(m[2], ".git"), Number: n, Forge: forgeGitHub, Tracker: trackerGitHub, MergeRequest: true, URL: strings.TrimSpace(s)}, true
}

func parseForgejoPullRequestRef(s string) (agentIssueRef, bool) {
	m := forgejoPullRequestRefRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return agentIssueRef{}, false
	}
	n, err := parsePositiveInt(m[3])
	if err != nil || n <= 0 {
		return agentIssueRef{}, false
	}
	return agentIssueRef{Owner: m[1], Repo: strings.TrimSuffix(m[2], ".git"), Number: n, Forge: forgeForgejo, Tracker: trackerForgejo, MergeRequest: true, URL: strings.TrimSpace(s)}, true
}

// looksLikeExplicitForgejoIssueRef reports whether s names Forgejo directly rather
// than relying on compact owner/repo syntax.
func looksLikeExplicitForgejoIssueRef(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	if strings.Contains(s, "://") {
		return true
	}
	base := strings.ToLower(strings.TrimRight(forgejoBaseURL, "/"))
	return strings.HasPrefix(s, base+"/") || strings.HasPrefix(s, "www."+base+"/")
}

func parseDispatchIssueRef(s string) (agentIssueRef, error) {
	ref, err := ParseIssueRef(forgejoBaseURL, s)
	if err != nil {
		return agentIssueRef{}, err
	}
	return agentIssueRef{
		Owner:   ref.Owner,
		Repo:    ref.Repo,
		Number:  ref.Number,
		Forge:   dispatchPlatformToForge(ref.Platform),
		Tracker: dispatchPlatformToTracker(ref.Platform),
	}, nil
}

func dispatchPlatformToForge(p Platform) forge {
	if p == PlatformGitHub {
		return forgeGitHub
	}
	return forgeForgejo
}

func dispatchPlatformToTracker(p Platform) tracker {
	if p == PlatformGitHub {
		return trackerGitHub
	}
	return trackerForgejo
}

// resolveAgentIssueRef parses the ref and, for a bare #N / N, fills owner/repo from
// the cwd's git origin via resolveTarget - the inference ask/task use (ward#282).
func (r *Runner) resolveAgentIssueRef(ctx context.Context, arg string) (agentIssueRef, error) {
	ref, err := parseAgentIssueRef(arg)
	if err != nil {
		return agentIssueRef{}, err
	}
	if ref.Owner != "" && ref.Repo != "" {
		if ref.Forge == 0 {
			ref.Forge = currentSmartDefaults().forgeForRepo(ref.Owner, ref.Repo)
		}
		kind := "issue ref"
		if ref.MergeRequest {
			kind = "pull request ref"
		}
		writef(os.Stderr, "ward agent: resolved %s %s -> %s\n", kind, arg, ref)
		return ref, nil
	}
	repo, _, terr := r.resolveTarget(ctx, "")
	if terr != nil {
		return agentIssueRef{}, fmt.Errorf(
			"bare issue ref %q needs a repo, but the current directory has no git origin to infer one from "+
				"(use owner/repo#%d or run from inside the repo's checkout): %w", arg, ref.Number, terr)
	}
	ref.Owner, ref.Repo = repo.Owner, repo.Name
	if ref.Forge == 0 {
		ref.Forge = currentSmartDefaults().forgeForRepo(ref.Owner, ref.Repo)
	}
	kind := "bare issue ref"
	if ref.MergeRequest {
		kind = "bare pull request ref"
	}
	writef(os.Stderr, "ward agent: inferred %s %s -> %s from cwd origin\n", kind, arg, ref)
	return ref, nil
}

// markdownImageRE matches inline ![alt](url) image embeds.
var markdownImageRE = regexp.MustCompile(`!\[[^\]]*\]\([^)\s]*\)`)

// bareImageURLRE matches a standalone http(s) URL ending in a common image
// extension (with an optional query string), e.g. a pasted screenshot link.
var bareImageURLRE = regexp.MustCompile(`(?i)https?://\S+?\.(?:png|jpe?g|gif|webp|bmp|svg|tiff?)(?:\?\S*)?`)

// emptyBodySeedAction is the first move when an issue has no body: work from the
// title, don't go hunting content that isn't there (ward#157). See docs/agent.md.
const emptyBodySeedAction = "This issue has no body, so work from the title alone - do not hunt for " +
	"issue content, screenshots, or other artifacts that are not there (an empty body is not an " +
	"invitation to invent one). The comment thread at that URL may hold later context worth a quick read."

// TODO(ward#792): remove this emergency default once brokered QA replaces the
// in-container review gate.
const temporaryReviewGateSkipReason = "the temporary ward default pending brokered QA"

func reviewGateDisabledByTemporaryDefault(role string) bool {
	return role == "engineer"
}

// headlessReflection is the headless run's closing "how it felt" retro led by a
// WARD-OUTCOME line; its landing phrase is workflow-aware (ward#281, ward#508).
func headlessReflection(ref agentIssueRef, wf workflowMode, reviewGate bool, reviewSkip string) string {
	outcomeStatus := workflowOutcomeStatus(wf, reviewGate)
	workflowLine := "workflow: <mode>; review summary: <summary or skip state>"
	landingPhrase := workflowLandingPhrase(ref, wf)
	if canonicalWorkflow(wf.orDefault()) == workflowPullRequestAndMerge {
		workflowLine = "workflow: pull-request-and-merge; review summary: <summary or skip state>"
	}
	reviewLine := "If a review ran, read `~/.ward/review-summary.txt` and copy its exact one-line summary into the same final comment."
	if !reviewGate {
		reviewLine = "The in-container review gate was intentionally skipped"
		if reviewSkip = strings.TrimSpace(reviewSkip); reviewSkip != "" {
			reviewLine += " because " + reviewSkip
		}
		reviewLine += ", so the final comment must say that explicitly."
		if canonicalWorkflow(wf.orDefault()) == workflowPullRequestAndMerge {
			landingPhrase = "the pull request is open and the review gate was intentionally skipped"
		}
	}
	return "Finally, as your very last step - only after " + landingPhrase + " - post one hypercurt " +
		"comment on this issue. The only visible text before the collapsed block is a single machine-readable " +
		"status line - its very first visible line, exactly one of:\n" +
		"  `" + wardOutcomeMarker + " " + outcomeStatus + "`\n" +
		"  `" + wardOutcomeMarker + " blocked 🛑`\n" +
		"  `" + wardOutcomeMarker + " failed ❌`\n" +
		"Put every other word inside one collapsed `<details><summary>details</summary>` block: the review " +
		"summary or skip state, the workflow line (`" + workflowLine + "`), " +
		"the short candid retrospective on how the implementation \"felt\", confidence, surprises, and follow-ups. Do not leave " +
		"any visible prose outside that first status line. " + reviewLine + " " + headlessWorkflowFailureCommentClause(ref, wf) + " A supervising director loop " +
		"(ward agent director) reads only that first line to classify the run, so for a normal run that completed " +
		"its workflow it is `" + wardOutcomeMarker + " " + outcomeStatus + "`. Reserve blocked/failed for a run that genuinely could not land."
}

func headlessWorkflowFailureCommentClause(ref agentIssueRef, wf workflowMode) string {
	mode := canonicalWorkflow(wf.orDefault())
	if mode == workflowPullRequest || mode == workflowPullRequestAndMerge {
		return workflowFailureCommentClauseFor(ref.Forge)
	}
	return ""
}

// reviewGateClause wires the pre-landing adversarial review panel into a headless
// seed (ward#134): run `ward agent review` before landing. docs/dispatch-review.md.
func reviewGateClause(ref agentIssueRef, wf workflowMode) string {
	noun := workflowReviewNoun(ref.Forge)
	landing := "open the " + noun
	switch mode := string(canonicalWorkflow(wf.orDefault())); mode {
	case string(workflowDirectToMain):
		landing = "merge to `main`"
	case string(workflowPullRequest):
		landing = "open the " + noun
	case string(workflowPullRequestAndMerge):
		landing = "merge the " + noun
	case string(workflowRemoteBranchOnly):
		landing = "push the remote branch"
	}
	var workflowTail string
	switch mode := string(canonicalWorkflow(wf.orDefault())); mode {
	case string(workflowDirectToMain):
		workflowTail = "For `merge-remote-main` workflows, landing means merging to `main`. Do not stop before the merge lands."
	case string(workflowPullRequest):
		workflowTail = "For `pull-request` workflows, opening the " + noun + " is not a stopping point. Keep watching the " + noun + " checks after it opens. A failing check is not done: fetch the logs/status, patch the branch, push the update, and repeat until the " + noun + " is green or the failure is genuinely blocked."
	case string(workflowPullRequestAndMerge):
		workflowTail = "For `pull-request-and-merge` workflows, opening the " + noun + " is not a stopping point. Keep watching the " + noun + " checks and merge status after it opens. A failing check is not done: fetch the logs/status, patch the branch, push the update, and repeat until the " + noun + " is green and merged or the failure is genuinely blocked."
	case string(workflowRemoteBranchOnly):
		workflowTail = "For `remote-branch-only` workflows, the remote branch push is the finish line. Do not open a pull request and do not merge."
	default:
		workflowTail = "For `pull-request` workflows, opening the " + noun + " is not a stopping point. Keep watching the " + noun + " checks after it opens. A failing check is not done: fetch the logs/status, patch the branch, push the update, and repeat until the " + noun + " is green or the failure is genuinely blocked."
	}
	return fmt.Sprintf(
		"REVIEW GATE (ward#134): before you land this change (%s), and ONLY after CI is green, run the "+
			"in-container code-review pass:\n\n    ward agent review --ci-log <path-to-your-green-ci-output>\n\n"+
			"It loads the hand-curated code-review skill from the companion aos checkout, starts with your own "+
			"harness family by default, and can escalate to other families only as a later, higher-cost fallback. "+
			"The reviewer works inside the worker container against the live filesystem state and prints a machine "+
			"line on stdout:\n"+
			"  - `WARD-REVIEW: pass ...`  -> you are cleared to land. Proceed.\n"+
			"  - `WARD-REVIEW: block ...` -> do NOT %s and do NOT merge. Post one hypercurt conclusion comment "+
			"starting with `WARD-OUTCOME: blocked 🛑` and put the reviewer verdicts, reasons, and review summary inside its collapsed details block. The gate is fail-closed - a "+
			"reviewer error or timeout is a block, not a pass.\n"+
			"  - `WARD-REVIEW: advisory ...` -> only if the gate had no runnable reviewer at all. Treat that as a "+
			"block, not a pass, and write the skip/availability summary into the conclusion comment so the issue shows "+
			"why the review could not run. `ward agent review` writes the exact one-line review summary to `~/.ward/review-summary.txt`; copy that line verbatim into the same conclusion comment.\n"+
			"%s\n"+
			"The gate's exit code mirrors the verdict (non-zero on block), so a shell `&&` also enforces it. Do "+
			"not skip it, and do not land on a block. If the review was intentionally skipped via `--skip-review`, "+
			"`--skip-preflight`, or config, the final `WARD-OUTCOME` comment must say so explicitly.",
		landing, landing, workflowTail)
}

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

// forgeDisplayName is the capitalized forge name the seed + prompts read with.
func forgeDisplayName(f forge) string {
	switch f {
	case forgeForgejo:
		return "Forgejo"
	case forgeGitHub:
		return "GitHub"
	case forgeGitLab:
		return "GitLab"
	}
	return "Forgejo"
}

// agentSeedPrompt seeds a merge-remote-main run (the default): a thin wrapper over
// agentSeedPromptWorkflow so legacy callers stay byte-for-byte (ward#405, ward#508).
func agentSeedPrompt(ref agentIssueRef, title, body, details string, headless bool, extra []targetRepo) string {
	return agentSeedPromptWorkflow(ref, title, body, details, headless, extra, defaultWorkflow, true, "")
}

// agentSeedPromptWorkflow is agentSeedPrompt with an explicit workflow mode (carry
// clause + landing phrase shift with it, ward#508) + a reviewGate toggle (ward#134).
func agentSeedPromptWorkflow(ref agentIssueRef, title, body, details string, headless bool, extra []targetRepo, wf workflowMode, reviewGate bool, reviewSkip string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "(untitled)"
	}
	body = strings.TrimSpace(body)

	action, inline := seedIssueBodyParts(body)
	kind := "issue"
	if ref.Forge == forgeGitLab && ref.MergeRequest {
		kind = "merge request"
	}
	seed := fmt.Sprintf(
		"Work on %s %s %s (%q).\n\n"+
			"URL: %s\n\n"+
			"%s\n\n%s\n\n%s Then carry it end to end per your container doctrine - %s",
		forgeDisplayName(ref.Forge), kind, ref, title, ref.url(), carryIssueBanner(ref), cloneAnchorLine(ref), action, workflowCarryClause(ref, wf))
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
	// Before landing, a headless run must clear the review gate (ward#134).
	// Remote-branch-only skips it because that workflow lands nothing else.
	if headless && reviewGate && wf.orDefault() != workflowRemoteBranchOnly {
		seed += "\n\n" + reviewGateClause(ref, wf)
	}
	// A headless run detaches unwatched, so it closes with a retrospective comment -
	// the only voice it leaves behind; the landing phrase tracks the workflow (#281, #508).
	if headless {
		seed += "\n\n" + headlessReflection(ref, wf, reviewGate && wf.orDefault() != workflowRemoteBranchOnly, reviewSkip)
	}
	return seed + inline
}

func seedIssueBodyParts(body string) (action, inline string) {
	switch {
	case body == "":
		return emptyBodySeedAction, ""
	case markdownImageRE.MatchString(body) || bareImageURLRE.MatchString(body):
		// Inline the body verbatim as a frozen snapshot for every driver, image markup
		// kept, URL live for comments + images (ward#400, ward#405).
		action = "The full issue body is inlined below, between the markers - work from that " +
			"frozen snapshot as your task text rather than re-fetching it. Images in the body are " +
			"linked by URL: fetch the URL to render them, and to read the live comment thread for " +
			"context added after dispatch. If your harness cannot read images, work from the " +
			"surrounding text."
	default:
		// No images: inline verbatim, URL only for the live comment thread.
		action = "The full issue body is inlined below, between the markers - work from that " +
			"frozen snapshot as your task text rather than re-fetching it. Fetch the URL only to read " +
			"the live comment thread for context added after dispatch."
	}
	inline = "\n\n----- issue body (inlined) -----\n" + body + "\n----- end issue body -----"
	return action, inline
}

// agentModes is the ordered set of harnesses ward can drive, derived from the
// embedded fleet config so the roster lives in one place.
var agentModes = mustAgentModes()

func mustAgentModes() []containerMode {
	// Init-time, so this reads ward's built-in frontier roster directly; a bad
	// WARD_CONFIG_REF must not affect the mode choice list (ward#653).
	out := make([]containerMode, 0, len(frontierAgentOrder))
	for _, name := range frontierAgentNames() {
		out = append(out, containerMode(name))
	}
	return out
}

// agentHarnessChoices renders the supported --harness values as a pipe list, e.g.
// "claude|codex|opencode|goose", for flag usage and error text.
func agentHarnessChoices() string {
	names := make([]string, 0, len(agentModes))
	for _, m := range agentModes {
		names = append(names, string(m))
	}
	return strings.Join(names, "|")
}

// agentHarnessFlags picks the harness driving a surface: --harness and --agent are
// equal first-class spellings (ward#660), --driver a one-release hidden deprecated alias.
func agentHarnessFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "harness",
			Value: string(defaultAgentMode()),
			Usage: "harness that drives the work: " + agentHarnessChoices() + " (default " + string(defaultAgentMode()) + ")",
		},
		&cli.StringFlag{
			Name: "agent",
			Usage: "equal spelling for --harness (ward#660): picks the same harness, " +
				"neither spelling is preferred",
		},
		&cli.StringFlag{
			Name:   "driver",
			Hidden: true,
			Usage: "deprecated alias for --harness/--agent (ward#660): kept one release cycle for " +
				"existing callers; an explicit first-class spelling wins when both are set",
		},
	}
}

// configFlag is the repeatable dotted-path model-context override (ward#616):
// `--config agent.<name>.<key>=<value>` rides in as the matching WARD_* env.
func configFlag() cli.Flag {
	return &cli.StringSliceFlag{
		Name:  "config",
		Usage: "override a resolved model-context knob (repeatable), e.g. --config agent.claude.model=sonnet --config agent.claude.effort=medium. Rides in as the matching WARD_* env; unknown keys fail loud (ward#616).",
	}
}

// Tailnet mechanism selectors for the hidden --tailnet-mode escape hatch (ward#362):
// auto picks by platform, the other two pin a mechanism.
const (
	tailnetModeAuto    = "auto"     // pick by platform: host-net on Linux, sidecar on Docker Desktop
	tailnetModeHostNet = "host-net" // force the --network=host route (ward#330)
	tailnetModeSidecar = "sidecar"  // force the ward-tailnet SOCKS5 sidecar route (ward#349)
)

// tailnetFlags carries the deprecated --tailnet alias + the hidden --tailnet-mode
// escape hatch (ward#362, ward#578; the role's guardfile set is the source now).
func tailnetFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:   "tailnet",
			Hidden: true,
			Usage: "deprecated (ward#578): tailnet reach is now a per-role guardfile set in ward-kdl.fleet.kdl; " +
				"this hidden alias force-joins the tailnet for one release, auto-selecting the mechanism by platform - " +
				"the host-network route on native Linux (ward#330), the SOCKS5 sidecar on Docker Desktop (ward#349)",
		},
		&cli.StringFlag{
			Name:   "tailnet-mode",
			Value:  tailnetModeAuto,
			Hidden: true,
			Usage:  "override the tailnet mechanism: auto|host-net|sidecar (default auto: pick by platform); force-joins the tailnet when set to a non-auto value (ward#362)",
		},
	}
}

// resolveTailnetMechanism maps a resolved tailnet want + --tailnet-mode to the
// host-net vs sidecar mechanism (ward#362): auto picks host-net on Linux, else sidecar.
func resolveTailnetMechanism(c *cli.Command, goos string, want bool) (hostNet, tsSidecar bool, err error) {
	if !want {
		return false, false, nil
	}
	switch mode := strings.TrimSpace(c.String("tailnet-mode")); mode {
	case "", tailnetModeAuto:
		if goos == "linux" {
			return true, false, nil
		}
		return false, true, nil
	case tailnetModeHostNet:
		return true, false, nil
	case tailnetModeSidecar:
		return false, true, nil
	default:
		return false, false, fmt.Errorf("invalid --tailnet-mode %q: want %s|%s|%s", mode, tailnetModeAuto, tailnetModeHostNet, tailnetModeSidecar)
	}
}

// extraRepoGrant reads the extra-writable-repo grant under either name: engineer's --repo
// or advisor/director's own --with-repo (ward#362 dropped the alias; nil-safe).
func extraRepoGrant(c *cli.Command) []string {
	return append(append([]string{}, c.StringSlice("repo")...), c.StringSlice("with-repo")...)
}

// agentHarness resolves the pick to a containerMode (default claude): --harness and
// --agent are equal spellings, --driver counts only when neither is set (ward#660).
func agentHarness(c *cli.Command) (containerMode, error) {
	raw, flag := c.String("harness"), "--harness"
	switch {
	case c.IsSet("harness") && c.IsSet("agent") && c.String("harness") != c.String("agent"):
		return "", fmt.Errorf("--harness %q and --agent %q disagree: they are equal spellings of the same pick (ward#660), set one",
			c.String("harness"), c.String("agent"))
	case !c.IsSet("harness") && c.IsSet("agent"):
		raw, flag = c.String("agent"), "--agent"
	case !c.IsSet("harness") && !c.IsSet("agent") && c.IsSet("driver"):
		raw, flag = c.String("driver"), "--driver"
	}
	m, err := parseMode(raw)
	if err != nil {
		return "", fmt.Errorf("invalid %s %q: want %s", flag, raw, agentHarnessChoices())
	}
	return m, nil
}

// surfaceDispatchMode resolves the harness for a director-surface sibling dispatch.
// Explicit flags win; otherwise inherit WARD_AGENT/WARD_MODE on brokered surfaces.
func surfaceDispatchMode(c *cli.Command) (containerMode, error) {
	mode, err := agentHarness(c)
	if err != nil {
		return "", err
	}
	if os.Getenv(envDispatchBrokerAddr) != "" && os.Getenv("WARD_READONLY") == "1" &&
		!c.IsSet("harness") && !c.IsSet("agent") && !c.IsSet("driver") {
		return currentAgentMode(), nil
	}
	return mode, nil
}

// agentCmdline renders the canonical `ward agent <surface> --harness <mode>` form
// (ward#185) for labels, provenance lines, and the re-dispatch hints ward prints.
func agentCmdline(mode containerMode, surface string) string {
	return fmt.Sprintf("ward agent %s --harness %s", surface, mode)
}

// agentCommand is the `ward agent` umbrella the `warded` public face fronts
// (ward#247, ward#282); a bare ref dispatches the default engineer (ward#347).
func agentCommand() *cli.Command {
	return &cli.Command{
		Name:   "agent",
		Usage:  "Send an agent into a fresh ephemeral container to carry the authoritative issue end to end (a bare ref runs the engineer).",
		Before: smartDefaultsGuard("ward agent"),
		Description: fmt.Sprintf(`agent is the issue-carrying dispatcher (the spelling 'warded' fronts), a
roster of startup roles (ward#347): you do not invoke a mode, you send in a
role. Pick a role (engineer|director|advisor|qa) and --harness picks the
harness (%s, default %s; --agent is an equal accepted spelling, --driver a
deprecated alias for one release, ward#660).
A BARE REF with no role word runs the 'engineer' role - the fire-and-forget
default. A bare #N (or N) infers the owner/repo from the cwd's git origin;
owner/repo#N resolves through the selected repo-authority policy, and a full
issue URL also works. One line replaces a full
container bring-up stack plus a prompt.

  warded coilyco-flight-deck/ward#98          # bare ref -> engineer run (warded face)
  warded #98                                  # owner/repo inferred from the cwd
  warded engineer #98                         # implement a ticket: detached fire-and-forget
  warded qa #98                               # structured QA verdict comment, no implementation edits
  warded engineer "fix the flaky exec_gate test" # freeform -> file an issue first, then carry
  warded <role> #98 --harness <harness>       # pick another harness
  warded <role> #98 --agent <harness>        # --agent: the same pick, equal spelling
  warded director --repo coilyco-flight-deck/ward # autonomous backlog supervisor (surfaces a read-only scope + dispatch session on drain)
  warded advisor #98 "what would it take to..."   # research the issue, post the answer
  warded advisor "how is the audit log written?"  # answer a freeform question inline
  ward agent engineer coilyco-flight-deck/ward#98 # the canonical spelling warded fronts
  ward agent #98 --print                      # resolve + show the plan, run nothing

See docs/agent.md for the warded face and docs/container.md for the container
model (ephemeral, fresh-clone-inside, reaper-backed). The agent runs under the
container's bypassPermissions policy, so a run is only accepted against a
trusted owner.`, agentHarnessChoices(), defaultAgentMode()),
		// The umbrella carries the engineer flag set + a default-role action so a
		// bare ref (the warded face, ward#282) runs the engineer; empty shows help.
		Flags:  agentEngineerFlags(),
		Action: agentDefaultSurfaceAction(),
		Commands: []*cli.Command{
			agentEngineerCommand(),
			agentDirectorCommand(),
			agentAdvisorCommand(),
			agentQACommand(),
			// roster is a self-describe verb, not a startup role: it prints the
			// flat list of the roles above (ward#348). See docs/agent-roster.md.
			agentRosterCommand(),
			// reap is a maintenance verb, not a startup role: the host-side
			// idle-killer for wedged engineer containers (#376). docs/agent-reap.md.
			agentReapCommand(),
			// stop is a control verb, not a startup role: a director surface stops
			// one running engineer through the dispatch broker (ward#627). docs/agent-stop.md.
			agentStopCommand(),
			// list is a read verb, not a startup role: a director surface lists
			// running engineer containers through the dispatch broker. docs/agent-list.md.
			agentListCommand(),
			// logs is a read verb, not a startup role: a director surface reads one
			// engineer's logs through the dispatch broker. docs/agent-logs.md.
			agentLogsCommand(),
			// dispatch-health is a read verb, not a startup role: it summarizes the
			// current pathology and feeds the Claude status line.
			agentDispatchHealthCommand(),
			// pr carries the native PR-workflow verbs (merge/status/runs/rerun), not a
			// startup role (ward#1067). docs/agent-pr-workflow.md.
			agentPRCommand(),
			// review is the pre-landing adversarial-review gate, not a startup role
			// (ward#134): a diff must survive a multi-model panel. docs/dispatch-review.md.
			agentReviewCommand(),
		},
	}
}

// agentDefaultSurfaceAction is the umbrella default: empty prints the role roster
// (ward#360), a parseable ref runs the engineer, else errors (ward#282, ward#347).
func agentDefaultSurfaceAction() cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() == 0 {
			// Truly-empty `warded` answers "who can I send in?" from the generated
			// roster (ward#360), not the flag dump; the bare-ref default is untouched.
			return agentRosterDefault(c)
		}
		arg := strings.TrimSpace(c.Args().First())
		if _, err := parseAgentIssueRef(arg); err != nil {
			return fmt.Errorf("unknown command %q for 'ward agent' (roles: %s); "+
				"a bare ref like #98 or owner/repo#N runs the engineer, and freeform work goes to "+
				"`ward agent engineer \"<instructions>\"`", arg, strings.Join(embeddedAgentRoleDefinitionOrder(), ", "))
		}
		return agentEngineerAction()(ctx, c)
	}
}

// agentImageFlags is the shared container image/ward-build/escalation flag block every
// dispatching role layers its own flags on top of (ward#355); --print stays per-role.
func agentImageFlags() []cli.Flag {
	// The image/ward-build/pinning group stays functional but hidden (ward#362).
	// --aws + the tailnet route are now hidden deprecated aliases (ward#578).
	flags := []cli.Flag{
		&cli.StringFlag{Name: "image", Value: agentImageDefault(), Hidden: true, Sources: cli.EnvVars(envAgentImage), Usage: "dev-base image to run (env: WARD_AGENT_IMAGE)"},
		&cli.StringFlag{Name: "tag", Value: agentTagDefault(), Hidden: true, Sources: cli.EnvVars(envAgentTag), Usage: "image tag; per-run pinning (env: WARD_AGENT_TAG)"},
		&cli.StringFlag{Name: "ward-source", Hidden: true, Usage: "development-only: mount a local ward checkout and build ward from it instead of downloading the release"},
		&cli.StringFlag{Name: "ward-version", Hidden: true, Sources: cli.EnvVars(envAgentVersion), Usage: "ward release the container downloads (default: this host's ward; env: WARD_AGENT_VERSION)"},
		&cli.BoolFlag{Name: "allow-ward-downgrade", Hidden: true, Usage: "permit a --ward-version pin older than this host's ward (ships an older in-container reaper; ward#529)"},
		&cli.BoolFlag{Name: "aws", Hidden: true, Usage: "deprecated (ward#578): the ~/.aws mount is now a per-role guardfile set (ward-kdl.aws.guardfile.kdl); this hidden alias force-mounts it read-only for one release"},
	}
	return append(flags, tailnetFlags()...)
}

// agentSurfaceFlags builds the detached launch flag set shared by the engineer,
// the bare-ref default, and the freeform task - no interactive surface here (ward#356).
func agentSurfaceFlags() []cli.Flag {
	flags := agentHarnessFlags()
	flags = append(flags,
		// --workflow picks the landing policy (ward#508).
		workflowFlag(),
		// --branch is hidden (ward#362): the issue-<N> default is the intelligent choice.
		&cli.StringFlag{Name: "branch", Hidden: true, Usage: "feature branch to create inside the clone (default: issue-<N>)"},
		&cli.StringSliceFlag{Name: "repo", Usage: "grant the agent an additional writable repo to clone + operate against (owner/name; repeatable). Cloned as a full feature copy under /workspace alongside the issue's repo (ward#230, ward#280)."},
		&cli.StringFlag{Name: "details", Usage: "extra operator instructions woven into the seeded prompt + pre-flight read (overrides the issue text on conflict)"},
		// --review-class tiers the pre-landing review panel and rides in as
		// WARD_REVIEW_CLASS (ward#134). See docs/dispatch-review.md.
		&cli.StringFlag{Name: "review-class", Usage: "autonomy class for the pre-landing review panel: lint-cleanup|default|refactor (default default; ward#134)"},
		&cli.BoolFlag{Name: "skip-review", Aliases: []string{"no-review-gate"}, Usage: "skip wiring the in-container review gate into the seed (ward#134); the run lands without the panel"},
		&cli.BoolFlag{Name: "github", Usage: "treat a bare owner/repo#N ref as a GitHub issue (clone/push + comments + PR on GitHub via a user-supplied token; ward#489). A github.com URL infers this automatically."},
		configFlag(),
	)
	flags = append(flags, agentImageFlags()...)
	flags = append(flags,
		&cli.BoolFlag{Name: "print", Usage: "resolve the issue + seeded prompt + docker plan and exit; inject no push token, run nothing"},
		// --no-pull is hidden (ward#362): a cached-image optimization, not everyday surface.
		&cli.BoolFlag{Name: "no-pull", Hidden: true, Usage: "skip the image pull (use the cached local image)"},
		// The --override-* family (ward#1045): two distinct escape hatches that never
		// imply each other - a hold may be stale, the OOM ceiling never is.
		&cli.BoolFlag{Name: "override-reservation", Usage: "skip the local + remote concurrency reservation checks (reclaim a stale or foreign per-issue hold); never overrides the pool ceiling"},
		&cli.BoolFlag{Name: "force", Hidden: true, Usage: "deprecated alias for --override-reservation (ward#1045); reclaims a reservation hold only, never the pool ceiling"},
		&cli.BoolFlag{Name: "override-capacity", Usage: "launch exactly one engineer past the OOM pool ceiling; the ceiling counts real running containers, so exceeding it risks host thrash or OOM (ward#1045)"},
	)
	// The detached run gets an autonomous pre-flight before launching (ward#137).
	// skip-preflight skips that, the launch-adjacent probes, and the review gate.
	flags = append(flags,
		&cli.BoolFlag{Name: "skip-preflight", Aliases: []string{"no-preflight"}, Usage: "skip the host pre-flight, reservation re-check wait, launch-adjacent network/image/update probes, and the in-container review gate, then detach immediately"},
		&cli.BoolFlag{Name: "skip-host-preflight", Hidden: true, Usage: "internal: skip only the host pre-flight; director auto-dispatch uses this so the review gate still runs"},
	)
	// --quiet-seed silences the seeded-prompt/issue-body stderr dump under director
	// auto-dispatch, whose console the in-process engineer shares (ward#519).
	flags = append(flags, &cli.BoolFlag{Name: "quiet-seed", Hidden: true, Usage: "suppress the seeded-prompt/issue-body dump to stderr (set by director auto-dispatch; the seed still rides into the container as its task text) - ward#519"})
	return flags
}

// preflightSkipped reports whether the operator asked to bypass the launch-adjacent
// preflight bucket (`--skip-preflight` or its alias).
func preflightSkipped(c *cli.Command) bool {
	return c.Bool("skip-preflight") || c.Bool("no-preflight")
}

// forceFlagDeprecationOnce keeps the --force deprecation notice to one line per
// process, however many gates read the flag on the way to a launch.
var forceFlagDeprecationOnce sync.Once

// overrideReservation reads --override-reservation or its deprecated --force alias
// (ward#1045, noticed once); it never implies --override-capacity, or vice versa.
func overrideReservation(c *cli.Command) bool {
	if c.Bool("force") {
		forceFlagDeprecationOnce.Do(func() {
			fmt.Fprintln(os.Stderr, "ward agent: --force is deprecated; use --override-reservation (ward#1045). It reclaims a reservation hold only - launching past the pool ceiling is --override-capacity, never implied by this flag.")
		})
	}
	return c.Bool("override-reservation") || c.Bool("force")
}

// resolvedWork bundles resolveAgentWork's output: ref, title, body, comment thread
// (ward#154), the --details note (ward#167), and the seeded prompt.
type resolvedWork struct {
	Ref        agentIssueRef
	Title      string
	Body       string
	Comments   []issueComment
	Details    string
	Seed       string
	Branch     string
	ReviewGate bool
	// ExtraRepos are the --repo grants the run also clones writable (ward#230);
	// the pre-flight must hear about them or it false-NO-GOs cross-repo work (ward#266).
	ExtraRepos []targetRepo
	// Workflow is the landing policy (--workflow, ward#508).
	// It shapes the seed, container env, and reaper gate.
	Workflow workflowMode
}

// agentPullRequestContext carries the extra seed metadata a PR-ref engineer run
// needs to start from the existing branch instead of treating it like fresh work.
type agentPullRequestContext struct {
	State        string
	Title        string
	Body         string
	URL          string
	HeadSHA      string
	HeadRef      string
	BaseRef      string
	Mergeability string
	RepairBucket string
	RepairNote   string
}

func (pr agentPullRequestContext) summaryLine() string {
	parts := []string{
		"source branch " + emptyDefault(pr.HeadRef, "(unknown)"),
		"base branch " + emptyDefault(pr.BaseRef, "(unknown)"),
		"mergeability " + emptyDefault(pr.Mergeability, "(unknown)"),
	}
	return strings.Join(parts, ", ")
}

type prContextTracker interface {
	getPullRequestContext(context.Context, string, string, int) (*agentPullRequestContext, error)
	listPullRequestComments(context.Context, string, string, int) ([]issueComment, error)
}

func joinNonEmptyBlocks(blocks ...string) string {
	var out []string
	for _, block := range blocks {
		if block = strings.TrimSpace(block); block != "" {
			out = append(out, block)
		}
	}
	return strings.Join(out, "\n\n")
}

func issueBodyWithComments(body string, comments []issueComment) string {
	body = strings.TrimSpace(body)
	thread := preflightComments(comments)
	if thread == "" {
		return body
	}
	if body != "" {
		return body + "\n\nComment thread (oldest first):\n\n" + thread
	}
	return "Comment thread (oldest first):\n\n" + thread
}

func engineerPRDetails(pr agentPullRequestContext, comments []issueComment, linkedIssue *Issue, linkedComments []issueComment) string {
	var b strings.Builder
	b.WriteString("PR continuation context. Treat this as repair or continuation work on an existing pull request, not fresh issue implementation.\n")
	if pr.Title != "" {
		fmt.Fprintf(&b, "- PR title: %s\n", pr.Title)
	}
	if pr.URL != "" {
		fmt.Fprintf(&b, "- PR URL: %s\n", pr.URL)
	}
	if pr.State != "" {
		fmt.Fprintf(&b, "- PR state: %s\n", pr.State)
	}
	fmt.Fprintf(&b, "- PR summary: %s\n", pr.summaryLine())
	if bucket := strings.TrimSpace(pr.RepairBucket); bucket != "" {
		fmt.Fprintf(&b, "- PR repair bucket: %s\n", bucket)
	}
	if note := strings.TrimSpace(pr.RepairNote); note != "" {
		fmt.Fprintf(&b, "- PR repair note: %s\n", note)
	}
	if pr.Body = strings.TrimSpace(pr.Body); pr.Body != "" {
		fmt.Fprintf(&b, "\n----- PR body -----\n%s\n----- end PR body -----\n", pr.Body)
	}
	if thread := preflightComments(comments); thread != "" {
		fmt.Fprintf(&b, "\n----- PR comment thread -----\n%s\n----- end PR comment thread -----\n", thread)
	}
	if linkedIssue != nil {
		fmt.Fprintf(&b, "\nLinked issue context: %d (%q)\nURL: %s\n", linkedIssue.Number, strings.TrimSpace(linkedIssue.Title), linkedIssue.URL)
		if linkedIssue.Body != "" {
			fmt.Fprintf(&b, "----- linked issue body -----\n%s\n----- end linked issue body -----\n", strings.TrimSpace(linkedIssue.Body))
		}
		if thread := preflightComments(linkedComments); thread != "" {
			fmt.Fprintf(&b, "----- linked issue comment thread -----\n%s\n----- end linked issue comment thread -----\n", thread)
		}
	}
	return strings.TrimSpace(b.String())
}

// resolveAgentWork parses + trust-gates the ref, fetches the issue (failing fast
// before any container spins), and returns the ref, title, body, and seed prompt.
func (r *Runner) resolveAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string) (resolvedWork, error) { //nolint:gocyclo,cyclop,gocognit,funlen
	label := agentCmdline(mode, surface)
	ref, err := r.resolveAgentIssueRef(ctx, c.Args().First())
	if err != nil {
		return resolvedWork{}, fmt.Errorf("%s: %w", label, err)
	}
	// --github forces a bare owner/repo#N onto the GitHub forge (a github.com URL
	// already parses there on its own; ward#489). See docs/agent-github.md.
	if c.Bool("github") {
		ref.Forge = forgeGitHub
	}
	if c.Bool("pr") {
		ref.MergeRequest = true
	}
	// Trust gate: the in-container agent runs under bypassPermissions, so only
	// spin one up for an owner in the trusted-owner set. Mirrors dispatch's check.
	if !r.ownerAllowed(ref.Owner) {
		return resolvedWork{}, r.untrustedOwnerErr(label, ref.Owner)
	}
	// Resolve the landing policy up front so a bad --workflow fails before any
	// container spins, and the seed carries the right carry clause (ward#508).
	wf, werr := agentWorkflow(c, ref.repoSlug())
	if werr != nil {
		return resolvedWork{}, fmt.Errorf("%s: %w", label, werr)
	}
	issue, issueErr := r.fetchIssue(ctx, ref)
	if issueErr != nil && !ref.MergeRequest {
		return resolvedWork{}, fmt.Errorf("%s: resolve issue %s: %w", label, ref, issueErr)
	}
	var title string
	var body string
	details := strings.TrimSpace(c.String("details"))
	var comments []issueComment
	var cerr error
	branch := ""
	if ref.MergeRequest { //nolint:nestif
		pr, prComments, prLinkedIssue, prLinkedComments, perr := r.resolveAgentPullRequestWork(ctx, mode, ref)
		if perr != nil {
			return resolvedWork{}, fmt.Errorf("%s: resolve pull request %s: %w", label, ref, perr)
		}
		state := pr.State
		if issueErr == nil && issue != nil {
			if strings.TrimSpace(issue.State) != "" {
				state = issue.State
			}
			if surface == "engineer" && issueHasModeLabel(issue.Labels, "interactive") {
				if !overrideReservation(c) && !c.Bool("print") {
					return resolvedWork{}, dispatchDeclineErr(dispatchModeCeiling, "mode-ceiling",
						"%s: refusing to dispatch the %s role on %s: the issue is explicitly labeled interactive - remove that label or pass --override-reservation to override (consult/default issues dispatch normally)",
						label, surface, ref)
				}
				writef(os.Stderr, "%s: note: issue %s is explicitly labeled interactive - dispatching anyway (--override-reservation/print).\n", label, ref)
			}
		}
		if st := strings.ToLower(strings.TrimSpace(state)); st != "" && st != "open" {
			if !overrideReservation(c) && !c.Bool("print") {
				return resolvedWork{}, dispatchDeclineErr(dispatchIssueClosed, "issue-closed",
					"%s: issue %s is %s, not open - nothing to do (pass --override-reservation to work it anyway)", label, ref, st)
			}
			writef(os.Stderr, "%s: note: issue %s is %s, not open - working it anyway (--override-reservation/--print).\n", label, ref, st)
		}
		title = strings.TrimSpace(pr.Title)
		body = strings.TrimSpace(pr.Body)
		comments = prComments
		branch = strings.TrimSpace(pr.HeadRef)
		details = joinNonEmptyBlocks(engineerPRDetails(pr, comments, prLinkedIssue, prLinkedComments), details)
	} else {
		if issueErr != nil {
			return resolvedWork{}, fmt.Errorf("%s: resolve issue %s: %w", label, ref, issueErr)
		}
		// Re-dispatch guard: a closed issue is already landed, so no-op instead of
		// rediscovering it; --override-reservation/--print work it anyway (ward#600).
		if st := strings.ToLower(strings.TrimSpace(issue.State)); st != "" && st != "open" {
			if !overrideReservation(c) && !c.Bool("print") {
				return resolvedWork{}, dispatchDeclineErr(dispatchIssueClosed, "issue-closed",
					"%s: issue %s is %s, not open - nothing to do (pass --override-reservation to work it anyway)", label, ref, st)
			}
			writef(os.Stderr, "%s: note: issue %s is %s, not open - working it anyway (--override-reservation/--print).\n", label, ref, st)
		}
		// Automation-mode gate: engineer dispatch only refuses issues explicitly
		// labeled interactive; consult/default issues are dispatchable (ward#663).
		if surface == "engineer" && issueHasModeLabel(issue.Labels, "interactive") {
			if !overrideReservation(c) && !c.Bool("print") {
				return resolvedWork{}, dispatchDeclineErr(dispatchModeCeiling, "mode-ceiling",
					"%s: refusing to dispatch the %s role on %s: the issue is explicitly labeled interactive - remove that label or pass --override-reservation to override (consult/default issues dispatch normally)",
					label, surface, ref)
			}
			writef(os.Stderr, "%s: note: issue %s is explicitly labeled interactive - dispatching anyway (--override-reservation/print).\n", label, ref)
		}
		title = strings.TrimSpace(issue.Title)
		body = strings.TrimSpace(issue.Body)
		// Fetch comments so the pre-flight sees decisions made there, not just the
		// body (ward#154); degrade to a body-only read on failure (the prior behavior).
		comments, cerr = r.fetchIssueComments(ctx, ref)
		if cerr != nil {
			writef(os.Stderr, "%s: note: could not read comments on %s (%v); pre-flight reads the body only\n", label, ref, cerr)
		}
	}
	// Resolve the --repo grants now so the pre-flight sees these repos too (ward#266,
	// ward#280; extraRepoGrant reads --repo, the --with-repo alias gone in ward#362).
	extra, eerr := parseExtraRepos(extraRepoGrant(c), targetRepo{Owner: ref.Owner, Name: ref.Repo})
	if eerr != nil {
		return resolvedWork{}, fmt.Errorf("%s: %w", label, eerr)
	}
	// The engineer detaches fire-and-forget (ward#356), so its seed always gets the
	// closing reflection - the only voice it leaves behind (ward#281).
	reviewGate, reviewSkip := reviewGateDecision(c, surface, mode, ref)
	if branch == "" {
		branch = fmt.Sprintf("issue-%d", ref.Number)
	}
	seedBody := body
	if !ref.MergeRequest {
		seedBody = issueBodyWithComments(body, comments)
	}
	seed := agentSeedPromptWorkflow(ref, title, seedBody, details, true, extra, wf, reviewGate, reviewSkip)
	seed += agentRunBudgetNote(roleEngineer)
	return resolvedWork{Ref: ref, Title: title, Body: seedBody, Comments: comments, Details: details, Seed: seed, Branch: branch, ExtraRepos: extra, Workflow: wf, ReviewGate: reviewGate}, nil
}

func (r *Runner) resolveAgentPullRequestWork(ctx context.Context, mode containerMode, ref agentIssueRef) (agentPullRequestContext, []issueComment, *Issue, []issueComment, error) {
	cl, err := r.hostTrackerClient(ctx, ref.trackerOrDefault(), mode)
	if err != nil {
		return agentPullRequestContext{}, nil, nil, nil, err
	}
	prc, ok := cl.(prContextTracker)
	if !ok {
		return agentPullRequestContext{}, nil, nil, nil, fmt.Errorf("tracker does not expose pull request context")
	}
	pr, err := prc.getPullRequestContext(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return agentPullRequestContext{}, nil, nil, nil, err
	}
	comments, err := prc.listPullRequestComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		writef(os.Stderr, "%s: note: could not read pull request comments on %s (%v); continuing with the PR body only\n", agentCmdline(mode, "engineer"), ref, err)
	}
	var linkedIssue *Issue
	var linkedComments []issueComment
	if linkedNum, ok := directorLinkedIssueNumber(pr.Body); ok && linkedNum > 0 && linkedNum != ref.Number {
		linkedRef := agentIssueRef{Owner: ref.Owner, Repo: ref.Repo, Number: linkedNum, Forge: ref.Forge, Tracker: ref.trackerOrDefault()}
		linkedIssue, err = r.fetchIssueByForge(ctx, agentCmdline(mode, "engineer"), ref.Forge, mode, linkedRef.Owner, linkedRef.Repo, linkedRef.Number)
		if err != nil {
			writef(os.Stderr, "%s: note: could not resolve linked issue %s (%v); continuing without linked issue context\n", agentCmdline(mode, "engineer"), linkedRef, err)
		} else {
			linkedComments, err = r.fetchIssueComments(ctx, linkedRef)
			if err != nil {
				writef(os.Stderr, "%s: note: could not read linked issue comments on %s (%v); continuing without them\n", agentCmdline(mode, "engineer"), linkedRef, err)
			}
		}
	}
	if fc, ok := cl.(*forgejoClient); ok {
		annotateForgejoPRRepair(ctx, fc, ref.Owner, ref.Repo, pr, ref, mode)
	}
	return *pr, comments, linkedIssue, linkedComments, nil
}

// fetchIssue reads the issue off the ref's tracker.
// It fails fast before a container launches.
func (r *Runner) fetchIssue(ctx context.Context, ref agentIssueRef) (*Issue, error) {
	cl, err := r.hostTrackerClient(ctx, ref.trackerOrDefault(), currentAgentMode())
	if err != nil {
		return nil, err
	}
	return resolveIssueWithRetry("ward agent", ref.String(), resolveIssueSleep, func() (*Issue, error) {
		return cl.getIssue(ctx, ref.Owner, ref.Repo, ref.Number)
	})
}

// resolveRetryAttempts / resolveRetryBackoff bound the dispatch-path resolve retry: a
// transient forge blip rode a bare `exit status 3` to a failed dispatch (ward#497).
const (
	resolveRetryAttempts = 3
	resolveRetryBackoff  = 2 * time.Second
)

// resolveIssueSleep is the backoff wait between resolve retries; a package var so a
// test swaps the real sleep out.
var resolveIssueSleep = time.Sleep

// resolveHTTPStatusRE pulls the 3-digit HTTP status from a folded resolve envelope:
// the Forgejo runtime writes "-> 404 ..." and `gh` writes "HTTP 404: ..." (ward#497).
var resolveHTTPStatusRE = regexp.MustCompile(`(?i)(?:->|\bHTTP(?:/\d(?:\.\d)?)?)\s+([1-5]\d\d)\b`)

// transientResolveErr reports whether a failed resolve is worth retrying: only a pinned
// 4xx (403 unreadable, 404 gone) is permanent, everything else retries (ward#497, docs).
func transientResolveErr(err error) bool {
	if err == nil {
		return false
	}
	if m := resolveHTTPStatusRE.FindStringSubmatch(err.Error()); m != nil && strings.HasPrefix(m[1], "4") {
		return false
	}
	return true
}

// resolveIssueWithRetry runs get up to resolveRetryAttempts times, backing off between
// transient failures (never after the last, never on a permanent 4xx). See ward#497.
func resolveIssueWithRetry(label, ref string, sleep func(time.Duration), get func() (*Issue, error)) (*Issue, error) {
	var issue *Issue
	var err error
	for attempt := 1; attempt <= resolveRetryAttempts; attempt++ {
		if issue, err = get(); err == nil {
			return issue, nil
		}
		if !transientResolveErr(err) {
			return nil, err
		}
		if attempt < resolveRetryAttempts {
			writef(os.Stderr, "%s: note: resolving issue %s hit a transient failure on attempt %d/%d (%v); retrying in %s\n",
				label, ref, attempt, resolveRetryAttempts, err, resolveRetryBackoff)
			sleep(resolveRetryBackoff)
		}
	}
	return nil, fmt.Errorf("after %d transient attempt(s): %w", resolveRetryAttempts, err)
}

// fetchIssueComments returns the comment thread (oldest first) for the pre-flight
// read via the ref's tracker client; caller degrades gracefully on error.
func (r *Runner) fetchIssueComments(ctx context.Context, ref agentIssueRef) ([]issueComment, error) {
	cl, err := r.hostTrackerClient(ctx, ref.trackerOrDefault(), currentAgentMode())
	if err != nil {
		return nil, err
	}
	return cl.listIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
}

// The automation-mode gate (ward#663): ward's own dispatch path only refuses an
// engineer dispatch when the issue is explicitly labeled interactive.

// modeCeilingLevels lists the automation-mode labels low-to-high by autonomy; the
// index is the level, so the last entry (headless) is the most autonomous.
var modeCeilingLevels = []string{"consult", "interactive", "headless"}

// modeCeilingLevel returns the rank of a mode label and whether it is a known one.
func modeCeilingLevel(label string) (int, bool) {
	want := strings.ToLower(strings.TrimSpace(label))
	for i, l := range modeCeilingLevels {
		if l == want {
			return i, true
		}
	}
	return 0, false
}

// issueModeCeiling returns the ceiling an issue's labels grant (rank + name) like
// cli-guard: unlabeled fails closed to consult, several take the lowest (#246).
func issueModeCeiling(labels []string) (int, string) {
	level, name, found := 0, "consult (unlabeled default)", false
	for _, raw := range labels {
		lv, ok := modeCeilingLevel(raw)
		if !ok {
			continue
		}
		if !found || lv < level {
			level, name = lv, modeCeilingLevels[lv]
		}
		found = true
	}
	return level, name
}

// issueHasModeLabel reports whether the issue carries the given label.
func issueHasModeLabel(labels []string, want string) bool { //nolint:unparam
	for _, raw := range labels {
		if strings.EqualFold(strings.TrimSpace(raw), want) {
			return true
		}
	}
	return false
}

// reviewGateWanted decides whether the review gate wires into the seed.
// CLI skips override config skips, and --no-preflight / --skip-preflight skip review too.
func reviewGateWanted(c *cli.Command, worker containerMode, ref agentIssueRef) bool {
	wanted, _ := reviewGateDecision(c, "engineer", worker, ref)
	return wanted
}

// reviewGateDecision resolves whether the in-container review gate should run and, if
// not, why it was skipped.
func reviewGateDecision(c *cli.Command, role string, worker containerMode, ref agentIssueRef) (bool, string) {
	if c.Bool("skip-review") || c.Bool("no-review-gate") {
		return false, "review gate skipped by --skip-review / --no-review-gate"
	}
	if preflightSkipped(c) {
		return false, "review gate skipped because --skip-preflight / --no-preflight also skips review"
	}
	skips, err := loadReviewSkips()
	if err != nil {
		writef(os.Stderr, "ward agent: note: could not read review skip defaults: %v\n", err)
		skips = nil
	}
	if reviewSkipMatches(skips, role, string(worker), ref.repoSlug()) {
		return false, "review gate skipped by ~/.ward/config.yaml default"
	}
	if reviewGateDisabledByTemporaryDefault(role) {
		return false, temporaryReviewGateSkipReason
	}
	return true, ""
}

func (r *Runner) writeSkippedReviewSummaryHandoff(mode containerMode, skipReason string) {
	if skipReason == "" {
		return
	}
	skipRes := reviewpanel.PanelResult{Worker: string(mode), Gate: reviewpanel.GateAdvisory, Note: skipReason}
	if werr := writeReviewSummaryHandoff(skipRes); werr != nil {
		if path, perr := reviewSummaryPath(); perr == nil {
			writef(r.Runner.Stderr, "ward agent: WARNING: could not write skipped-review summary handoff %s: %v\n", path, werr)
		}
	}
}

// reviewSkipMatches reports whether any configured skip rule matches the active
// role, worker harness, or repo slug.
func reviewSkipMatches(rules []string, role, worker, repo string) bool {
	for _, raw := range rules {
		if reviewSkipMatch(raw, role, worker, repo) {
			return true
		}
	}
	return false
}

func reviewSkipMatch(raw, role, worker, repo string) bool {
	rule := strings.TrimSpace(raw)
	if rule == "" {
		return false
	}
	kind, value, ok := strings.Cut(rule, ":")
	if !ok {
		kind = ""
		value = rule
	} else {
		kind = strings.ToLower(strings.TrimSpace(kind))
		value = strings.TrimSpace(value)
	}
	match := func(left, right string) bool {
		return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	switch kind {
	case "role":
		return match(role, value)
	case "agent", "harness":
		return match(worker, value)
	case "repo":
		return match(repo, value)
	case "":
		return match(role, value) || match(worker, value) || match(repo, value)
	default:
		return false
	}
}

// runAgentWork resolves the issue, seeds the prompt, runs the autonomous
// pre-flight (runPreflight), and launches the detached run (ward#356).
func (r *Runner) runAgentWork(ctx context.Context, c *cli.Command, mode containerMode, surface string) error {
	label := agentCmdline(mode, surface)
	w, err := r.resolveAgentWork(ctx, c, mode, surface)
	if err != nil {
		return err
	}
	if err := r.runAgentWorkPreLaunchChecks(ctx, c, mode, surface, label, w); err != nil {
		return err
	}
	workerName := issueScopedContainerName(roleEngineer, mode, targetRepo{Owner: w.Ref.Owner, Name: w.Ref.Repo}, w.Ref.Number)
	if err := r.precheckLiveIssueWorker(ctx, label, w.Ref, workerName, overrideReservation(c)); err != nil {
		return err
	}
	var justification string
	if preflightWanted(c) {
		// ward#184: gate on the cheap, authoritative reservation before a full LLM
		// pre-flight is spent on an issue another run holds. See docs/agent.md.
		if perr := r.precheckReservation(ctx, agentCmdline(mode, surface), w, overrideReservation(c)); perr != nil {
			return perr
		}
		proceed, read, perr := r.runPreflight(ctx, mode, surface, w)
		if perr != nil {
			// A Coded decline (NO-GO / WRONG-REPO, already reported) or a plain
			// execution error; both surface as-is for the right exit code (ward#485).
			return perr
		}
		if !proceed {
			// Defensive: a non-proceed with no error shouldn't happen (declines carry
			// perr now), but never launch when the pre-flight said not to.
			return nil
		}
		// On a GO, carry the read into the reservation comment (ward#383).
		justification = read
	}
	return r.launchAgentContainer(ctx, c, mode, surface, w, justification)
}

func (r *Runner) runAgentWorkPreLaunchChecks(ctx context.Context, c *cli.Command, mode containerMode, surface, label string, w resolvedWork) error {
	if c.Bool("print") {
		return nil
	}
	// Warn at host dispatch if ward is stale; a detached run buries the only
	// `ward version` signal in a container log (ward#143).
	if preflightSkipped(c) {
		writef(os.Stderr, "%s: skipping ward update reminder (--skip-preflight)\n", label)
	} else {
		r.maybeWarnWardOutdated(ctx)
	}
	if !w.ReviewGate {
		r.maybeWriteSkippedReviewSummaryHandoff(mode, c, surface, w.Ref)
	}
	return r.maybeLaunchOpenPRBackpressure(ctx, label, w.Ref.repoSlug(), c, w)
}

func (r *Runner) maybeWriteSkippedReviewSummaryHandoff(mode containerMode, c *cli.Command, surface string, ref agentIssueRef) {
	if _, skipReason := reviewGateDecision(c, surface, mode, ref); skipReason != "" {
		r.writeSkippedReviewSummaryHandoff(mode, skipReason)
	}
}

// preflightTimeout caps the pre-flight read so a wedged agent can't hold the
// operator's terminal hostage before the real run even starts.
const preflightTimeout = 3 * time.Minute

// preflightWanted gates the pre-flight to an interactive dispatch (a human at the
// terminal who walked away), never --print, honoring --skip-preflight. See docs.
func preflightWanted(c *cli.Command) bool {
	return terminalAttached() && !c.Bool("print") && !preflightSkipped(c) && !c.Bool("skip-host-preflight")
}

// launchPreflightSkipReasons lists the launch-adjacent probes skipped by
// --skip-preflight for the current plan.
func launchPreflightSkipReasons(plan upPlan, noPull bool) []string {
	if !plan.SkipPreflight {
		return nil
	}
	reasons := []string{}
	if plan.TSSidecar {
		reasons = append(reasons, "ward-tailnet readiness check")
	}
	if !noPull {
		reasons = append(reasons, "image pull")
	}
	return reasons
}

func logLaunchPreflightSkips(label string, plan upPlan, noPull bool) {
	if !plan.SkipPreflight {
		return
	}
	for _, reason := range launchPreflightSkipReasons(plan, noPull) {
		writef(os.Stderr, "%s: skipping %s (--skip-preflight)\n", label, reason)
	}
}

func logLaunchImageDecision(label string, plan upPlan, noPull bool) {
	switch {
	case plan.SkipPreflight && !noPull:
		writef(os.Stderr, "%s: skipping image pull (--skip-preflight)\n", label)
	case !plan.SkipPreflight && !noPull:
		writef(os.Stderr, "%s: image pull enabled for %s\n", label, plan.Image)
	}
}

func appendLaunchPreflightNotes(b *strings.Builder, plan upPlan, noPull bool) {
	if !plan.SkipPreflight {
		return
	}
	writef(b, "# skip-preflight: launch-adjacent probes are bypassed before the container starts; trust and closed-issue checks still run\n")
	for _, reason := range launchPreflightSkipReasons(plan, noPull) {
		writef(b, "# skip-preflight: skipping %s\n", reason)
	}
	writef(b, "# pull skipped (--skip-preflight); image: %s\n", plan.Image)
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
			"this issue end to end on your own - implement, commit, merge to main, "+
			"push - with no human watching once you detach.\n\n"+
			"That detached run happens in %s pulled inside the container. "+
			"The directory you are reading this in right now is unrelated host scratch - it may "+
			"hold a different repo, or none at all. So judge feasibility from the issue text "+
			"alone, never from the local working tree: a file, path, or package that looks "+
			"missing in the current directory tells you nothing about the clone you will actually "+
			"get, so do not conclude the issue is mis-filed just because the local tree lacks it.%s\n\n"+
			"Important context: this pre-flight read happens in a temporary host scratch directory. "+
			"The work itself will take place in %s. This is the repository you should explore for any needed "+
			"conventions, schemas, file layouts, or wiring patterns required to complete this task.%s\n\n"+
			"%s\n\n"+
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
		cloneScope, extraNote, cloneScope, extraNote, carryIssueBanner(ref), ref, title, body, note, thread, gate, ref.Owner, ref.Repo)
}

// preflightStripsComment reports whether the pre-flight read drops this comment as
// ward's own bookkeeping (reservation pings, NO-GO verdicts) or empty (ward#154).
func preflightStripsComment(c issueComment) bool {
	return strings.TrimSpace(c.Body) == "" ||
		strings.Contains(c.Body, agentReservationMarker) ||
		strings.Contains(c.Body, preflightNoGoMarker)
}

// preflightComments renders the human comment thread (oldest first) for the
// pre-flight, dropping ward's own bookkeeping so only human words sway it (see docs).
func preflightComments(comments []issueComment) string {
	var b strings.Builder
	for _, c := range comments {
		if preflightStripsComment(c) {
			continue
		}
		body := strings.TrimSpace(c.Body)
		who := strings.TrimSpace(c.User.Login)
		if who == "" {
			who = "(unknown author)"
		}
		writef(&b, "--- comment by %s (%s) ---\n%s\n\n", who, c.CreatedAt.Format(time.RFC3339), body)
	}
	return strings.TrimSpace(b.String())
}

// capturePreflight runs the feasibility-read argv in a fresh empty temp dir so it never
// inherits the dispatch cwd (ward#169); the prompt rides the child's stdin (ward#548).
func (r *Runner) capturePreflight(ctx context.Context, argv []string, stdin string) ([]byte, error) {
	// No temp dir means no isolation, but the prompt lever still stands: fall back
	// to a plain cwd capture rather than strand a workable issue behind flakiness.
	dir, err := os.MkdirTemp("", "ward-preflight-*")
	if err != nil {
		writeln(os.Stderr, "ward agent: preflight capture could not create a neutral temp dir; falling back to dispatch cwd")
		return r.captureWithStdin(ctx, stdin, argv[0], argv[1:]...)
	}
	writef(os.Stderr, "ward agent: preflight capture start in neutral dir %s\n", dir)
	defer os.RemoveAll(dir)
	out, cerr := r.captureInDir(ctx, dir, stdin, argv[0], argv[1:]...)
	if cerr != nil {
		writef(os.Stderr, "ward agent: preflight capture failed in %s: %v\n", dir, cerr)
		return out, cerr
	}
	writef(os.Stderr, "ward agent: preflight capture done in %s\n", dir)
	return out, nil
}

// captureInDir runs Capture with the process cwd temporarily set to dir, restored
// afterward (cli-guard's Capture has no Dir knob). A guarded chdir is safe here.
func (r *Runner) captureInDir(ctx context.Context, dir, stdin, bin string, argv ...string) ([]byte, error) {
	// The pre-flight is a sequential host one-shot, so no concurrent cwd user can
	// race this; a failed Getwd/Chdir simply no-ops to a plain cwd capture.
	if prev, err := os.Getwd(); err == nil {
		if cerr := os.Chdir(dir); cerr == nil {
			defer os.Chdir(prev) //nolint:errcheck // best-effort restore
		}
	}
	return r.captureWithStdin(ctx, stdin, bin, argv...)
}

// captureWithStdin feeds prompt on the child's stdin for one capture, then restores
// the Runner's stdin (the host one-shot is sequential, so the field swap can't race).
func (r *Runner) captureWithStdin(ctx context.Context, stdin, bin string, argv ...string) ([]byte, error) {
	prev := r.Runner.Stdin
	r.Runner.Stdin = strings.NewReader(stdin)
	defer func() { r.Runner.Stdin = prev }()
	return r.Runner.Capture(ctx, bin, argv...)
}

// runPreflight acts on the agent's feasibility verdict with no human, shared by
// the headless + task surfaces (ward#147, ward#149): only NO-GO blocks. See docs.
func (r *Runner) runPreflight(ctx context.Context, mode containerMode, surface string, w resolvedWork) (bool, string, error) {
	label := agentCmdline(mode, surface)
	bin := lookupAgent(mode).Record().Binary
	argv, stdin, ok := hostOneShot(mode, preflightPrompt(w.Ref, w.Title, w.Body, w.Details, w.Comments, w.ExtraRepos))
	// No host one-shot (none wired, or a local-model harness barred from the
	// unsandboxed host read; ward#162) or no binary: proceed to the isolated run.
	if !ok || !hostHasBinary(bin) {
		if !hostOneShotTrusted(mode) {
			writef(os.Stderr, "%s: %s is a local-model harness - skipping the unsandboxed host pre-flight and going straight to the isolated container run (ward#162).\n", label, bin)
		} else {
			writef(os.Stderr, "%s: %s self-assessment unavailable on this host; proceeding with the detached run.\n", label, bin)
		}
		return true, "", nil
	}

	writef(os.Stderr, "%s: preflight start for %s via %s\n", label, w.Ref, bin)
	writef(os.Stderr, "%s: pre-flight - asking %s whether it can carry %s before detaching...\n\n", label, bin, w.Ref)
	pctx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()
	// Capture (not Exec) so ward can read the verdict; the read is echoed below.
	// capturePreflight isolates it in a neutral dir, never the dispatch cwd (#169).
	out, err := r.capturePreflight(pctx, argv, stdin)
	read := strings.TrimSpace(string(out))
	if read != "" {
		writef(os.Stderr, "%s\n\n", read)
	}
	if err != nil {
		// A read that didn't complete is not the agent saying no: fail open so a
		// flaky host agent never strands an otherwise-workable issue.
		writef(os.Stderr, "%s: pre-flight read did not complete (%v); proceeding with the detached run.\n", label, err)
		return true, "", nil
	}
	outcome := parsePreflightVerdict(read)
	writef(os.Stderr, "%s: preflight result for %s verdict=%v repo=%q reason=%q\n", label, w.Ref, outcome.Verdict, outcome.Repo, outcome.Reason)

	switch outcome.Verdict {
	case verdictWrongRepo:
		// WRONG-REPO always launches nothing here (it either blind-fires elsewhere
		// or bounces to a human), so proceed is false regardless of the error.
		return false, "", r.handlePreflightWrongRepo(ctx, mode, surface, w, outcome, read)
	case verdictNoGo:
		writef(os.Stderr, "%s: pre-flight NO-GO for %s; launching nothing, commenting on the issue.\n", label, w.Ref)
		if cerr := r.postPreflightNoGo(ctx, mode, surface, w.Ref, outcome.Reason, read); cerr != nil {
			return false, "", fmt.Errorf("post NO-GO comment on %s: %w", w.Ref, cerr)
		}
		writef(os.Stderr, "%s: commented NO-GO on %s - %s\n", label, w.Ref, w.Ref.url())
		return false, "", dispatchDeclineErr(dispatchNoGo, "preflight_no_go",
			"%s: pre-flight NO-GO for %s: %s", label, w.Ref, outcome.Reason)
	case verdictGo:
		// An explicit GO: proceed and hand the read back so the reservation comment
		// records the agent's own justification for carrying it (ward#383).
		writef(os.Stderr, "%s: preflight GO for %s\n", label, w.Ref)
		return true, read, nil
	case verdictUnknown:
		// No clear verdict line: proceed, but there is no GO conclusion to justify.
		writef(os.Stderr, "%s: preflight verdict unclear for %s; proceeding open\n", label, w.Ref)
		return true, "", nil
	default:
		return true, "", nil
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
		reason := wrongRepoBounceReason(outcome, target, r.trustedOwners(), ok, sameRepo)
		writef(os.Stderr, "%s: pre-flight WRONG-REPO unusable for %s; bouncing to a human.\n", label, w.Ref)
		if cerr := r.postPreflightNoGo(ctx, mode, surface, w.Ref, reason, read); cerr != nil {
			return fmt.Errorf("post NO-GO comment on %s: %w", w.Ref, cerr)
		}
		// An unusable WRONG-REPO is a bounce-to-human, same ending as a NO-GO.
		return dispatchDeclineErr(dispatchNoGo, "preflight_wrong_repo_bounced",
			"%s: pre-flight WRONG-REPO for %s bounced to a human: %s", label, w.Ref, reason)
	}

	writef(os.Stderr, "%s: pre-flight WRONG-REPO for %s -> %s; blind-firing an issue there, launching nothing.\n", label, w.Ref, target.slug())
	// The blind-fire target lives on the same tracker as the source issue (ward#489).
	signed, err := r.hostTrackerClient(ctx, w.Ref.trackerOrDefault(), mode)
	if err != nil {
		return err
	}
	number, err := signed.createIssue(ctx, target.Owner, target.Name,
		w.Title, blindfireIssueBody(mode, surface, w, outcome.Reason))
	if err != nil {
		return fmt.Errorf("blind-fire issue into %s: %w", target.slug(), err)
	}
	filed := agentIssueRef{Owner: target.Owner, Repo: target.Name, Number: number, Forge: w.Ref.Forge, Tracker: w.Ref.trackerOrDefault()}
	writef(os.Stderr, "%s: blind-fired %s - %s\n", label, filed, filed.url())
	// Point the original issue at the freshly-filed one so the trail is visible.
	if cerr := signed.commentIssue(ctx, w.Ref.Owner, w.Ref.Repo, w.Ref.Number,
		preflightWrongRepoComment(mode, surface, filed, outcome.Reason, read)); cerr != nil {
		return fmt.Errorf("comment WRONG-REPO routing on %s: %w", w.Ref, cerr)
	}
	writef(os.Stderr, "%s: noted the routing on %s - %s\n", label, w.Ref, w.Ref.url())
	return dispatchDeclineErr(dispatchWrongRepo, "preflight_wrong_repo_routed",
		"%s: pre-flight WRONG-REPO for %s routed to %s (issue %s); launched nothing here",
		label, w.Ref, target.slug(), filed)
}

// hostHasBinary reports whether bin resolves on the host PATH.
func hostHasBinary(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// snapDockerBin returns the resolved docker path when the docker CLI on PATH is
// the snap package (its private /tmp breaks ward's launch; ward#557), else "".
func snapDockerBin(lookPath func(string) (string, error)) string {
	path, err := lookPath("docker")
	if err != nil {
		return ""
	}
	if dockerPathIsSnap(path) {
		return path
	}
	return ""
}

// dockerPathIsSnap reports whether a resolved docker path is the snap package -
// /snap/bin/docker, or a PATH shim whose symlink chain ends at the snap wrapper.
func dockerPathIsSnap(path string) bool {
	if pathUnderSnap(path) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return pathUnderSnap(resolved) || filepath.Base(resolved) == "snap"
}

// pathUnderSnap reports whether p is the /snap tree itself or a path within it.
func pathUnderSnap(p string) bool {
	p = filepath.Clean(p)
	return p == "/snap" || strings.HasPrefix(p, "/snap/")
}

// snapDockerRemediation names the cause (snap's private /tmp) and the fix (native
// docker-ce), making the raw exit-125 ENOENT actionable. docs/container-env.md.
func snapDockerRemediation(path string) string {
	return fmt.Sprintf(
		"ward container: docker on PATH is the snap package (%s), which runs the docker CLI under a "+
			"private /tmp and connects only snap's `home` interface (non-hidden $HOME files). Ward must "+
			"hand docker several host paths that snap docker cannot reach - the --env-file, the -v "+
			"assets/context bind mounts, the clone bind, and the broker socket - so a container launch "+
			"dies mid-run at `docker run` with exit 125 (\"open ...: no such file\"). Snap docker cannot "+
			"expose /tmp or dot-dirs to close this, so the fix is a native docker: install docker-ce from "+
			"Docker's apt repo (https://docs.docker.com/engine/install/) and put /usr/bin/docker ahead of "+
			"/snap/bin on PATH, then re-run. (ward#557)",
		path)
}

// dispatchDockerState captures the signals deciding whether an in-container sibling
// dispatch can reach docker (ward#321); see docs/agent-surface.md.
type dispatchDockerState struct {
	inContainer  bool
	dockerOnPath bool
	brokerAddr   string
	readOnly     bool
}

// currentDispatchDockerState probes the live process for the dispatch signals.
func currentDispatchDockerState() dispatchDockerState {
	return dispatchDockerState{
		inContainer:  inContainer(),
		dockerOnPath: hostHasBinary("docker"),
		brokerAddr:   strings.TrimSpace(os.Getenv(envDispatchBrokerAddr)),
		readOnly:     os.Getenv("WARD_READONLY") == "1",
	}
}

// blocked reports whether this dispatch is doomed at the docker shell-out, with the
// loud reason if so: an in-container dispatch with no docker client (ward#321).
func (s dispatchDockerState) blocked() (bool, string) {
	if !s.inContainer || s.dockerOnPath {
		return false, ""
	}
	const base = "cannot dispatch a sibling run from inside this container: no docker client on PATH (explore can file but cannot dispatch)"
	var detail string
	switch {
	case s.brokerAddr != "" && !s.readOnly:
		detail = "a host dispatch broker is attached but WARD_READONLY is unset, so the broker forward was skipped and dispatch fell through to a docker client that is not installed"
	case s.brokerAddr != "":
		detail = "the host dispatch broker forward did not fire for this dispatch and no docker client is installed to fall back to"
	default:
		detail = "no host dispatch broker is attached (WARD_DISPATCH_BROKER_ADDR unset) and the image carries no docker client, so neither dispatch path is available"
	}
	return true, fmt.Sprintf("%s - %s. A director surface container dispatches over the host broker; a plain container needs a docker client in the image. See docs/agent-surface.md", base, detail)
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

// postPreflightNoGo comments the NO-GO verdict back on the issue (host tracker
// client, SSM-backed token), bouncing it to a human instead of failing silently.
func (r *Runner) postPreflightNoGo(ctx context.Context, mode containerMode, surface string, ref agentIssueRef, reason, read string) error {
	cl, err := r.hostTrackerClient(ctx, ref.trackerOrDefault(), mode)
	if err != nil {
		return err
	}
	return cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, preflightNoGoComment(mode, surface, reason, read))
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
	visible := "WARD-STATUS: pre-flight NO-GO 🛑"
	writef(&b, "`%s` ran a pre-flight feasibility read on this issue before "+
		"detaching a fire-and-forget run, and the agent judged it **NO-GO** - it should not be carried "+
		"unattended until a human weighs in.\n\n", agentCmdline(mode, surface))
	writef(&b, "> %s\n\n", reason)
	// Re-dispatch points at the `engineer`: the issue is already filed, so a
	// freeform engineer run would file a duplicate (ward#347).
	writef(&b, "No container was launched. Review the issue (clarify the scope, resolve the unknown, "+
		"or split it), then re-dispatch - `%s <ref> --skip-preflight` skips this gate "+
		"once you've decided it's good to go.\n", agentCmdline(mode, "engineer"))
	if read = strings.TrimSpace(read); read != "" {
		writef(&b, "\n## Full pre-flight read\n\n%s\n", read)
	}
	writef(&b, "\nPosted automatically by `%s` pre-flight (ward#147, ward#149).", agentCmdline(mode, surface))
	return preflightNoGoMarker + "\n" + collapsedIssueComment(visible, "pre-flight details", b.String())
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
	writef(&b, "Routed here from %s by `%s` pre-flight (ward#159): the feasibility "+
		"read judged this work belongs in this repo, not %s/%s.\n\n", w.Ref, agentCmdline(mode, surface), w.Ref.Owner, w.Ref.Repo)
	writef(&b, "> %s\n\n", reason)
	writef(&b, "This was filed blind from the source issue's text - nobody searched this repo first, "+
		"so confirm it fits before working it.\n\n")
	writef(&b, "---\n### Source issue (%s)\n\n%s\n", w.Ref, body)
	writef(&b, "\n---\nFiled automatically by `%s` pre-flight (ward#159).", agentCmdline(mode, surface))
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
	visible := "WARD-STATUS: pre-flight WRONG-REPO 🎯"
	writef(&b, "`%s` ran a pre-flight read on this issue and judged the work "+
		"belongs in **%s**, not here. Rather than burn cycles searching, it blind-fired a fresh "+
		"issue there:\n\n", agentCmdline(mode, surface), filed.repoSlug())
	writef(&b, "- %s - %s\n\n", filed, filed.url())
	writef(&b, "> %s\n\n", reason)
	writef(&b, "No container was launched here. If the routing is wrong, close %s and re-dispatch "+
		"this issue with `%s <ref> --skip-preflight` to skip the gate.\n", filed, agentCmdline(mode, "engineer"))
	if read = strings.TrimSpace(read); read != "" {
		writef(&b, "\n## Full pre-flight read\n\n%s\n", read)
	}
	writef(&b, "\nPosted automatically by `%s` pre-flight (ward#159).", agentCmdline(mode, surface))
	return collapsedIssueComment(visible, "pre-flight details", b.String())
}

// buildAgentPlan composes the detached container plan (seeded argv, issue-<N> branch,
// named container) for a resolved issue; it strips TTY flags so it never grabs a pty.
func buildAgentPlan(c *cli.Command, mode containerMode, ref agentIssueRef, branch string, seed string, assetsDir string) (upPlan, error) {
	cwd := resolveInvokeCWD()
	if cwd == "" {
		return upPlan{}, fmt.Errorf("cannot resolve the current directory")
	}
	repo := targetRepo{Owner: ref.Owner, Name: ref.Repo}
	plan, err := buildUpPlan(c, repo, mode, roleEngineer, cwd, assetsDir, []string{seed}, false)
	if err != nil {
		return upPlan{}, err
	}
	plan.Branch = strings.TrimSpace(branch)
	if plan.Branch == "" {
		plan.Branch = agentWorkBranch(resolvedWork{Ref: ref})
	}
	// Re-cast the session plan as an engineer: role-led name, unique by
	// repo+issue (ward#364). Issue also carries so the reaper can release it (ward#264).
	plan.Role = roleEngineer
	plan.Issue = ref.Number
	plan.Forge = ref.Forge
	// The landing policy rides the plan so it reaches the container env + label and
	// the reaper (ward#508); already validated upstream, so a parse slip defaults.
	plan.Workflow, _ = agentWorkflow(c, ref.repoSlug())
	// The review-panel class rides the plan into WARD_REVIEW_CLASS (ward#134);
	// validated here so a typo fails the dispatch loudly, not silently in-container.
	if rc := strings.TrimSpace(c.String("review-class")); rc != "" {
		if _, err := reviewpanel.ParseClass(rc); err != nil {
			return upPlan{}, err
		}
		plan.ReviewClass = rc
	}
	plan.Name = containerRoleName(roleEngineer, mode, repo, ref.Number, plan.Machine)
	plan.Headless = true
	plan.Interactive = false
	plan.TTY = false
	return plan, nil
}

func agentWorkBranch(w resolvedWork) string {
	if branch := strings.TrimSpace(w.Branch); branch != "" {
		return branch
	}
	return fmt.Sprintf("issue-%d", w.Ref.Number)
}

// seedLogBlock wraps the seeded prompt in greppable markers, shared by --print and the
// detached-run startup dump (ward#400); public issue text only, no tokens to spill.
func seedLogBlock(seed string) string {
	return fmt.Sprintf("----- seeded prompt -----\n%s\n----- end -----\n", seed)
}

// maybeDumpSeed writes the seed block to w for a killed run's audit record (ward#400),
// unless quiet - director auto-dispatch sets it to keep the dump off the console (#519).
func maybeDumpSeed(w io.Writer, seed string, quiet bool) {
	if quiet {
		return
	}
	writeln(w, seedLogBlock(seed))
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

// launchAgentContainer turns a resolved (ref, title, seed) into the container plan and
// fires it detached - the shared tail of engineer, freeform task, and route (ward#356).
func (r *Runner) launchAgentContainer(ctx context.Context, c *cli.Command, mode containerMode, surface string, w resolvedWork, justification string) error { //nolint:gocyclo,cyclop,gocognit,funlen
	label := agentCmdline(mode, surface)
	ref, title, seed := w.Ref, w.Title, w.Seed

	// Fail a doomed in-container dispatch loudly at bring-up, not at the raw
	// `exec: "docker"` lookup later (ward#321); --print warns but still renders.
	if blocked, reason := currentDispatchDockerState().blocked(); blocked {
		if c.Bool("print") {
			writef(os.Stderr, "%s: warning: %s\n", label, reason)
		} else {
			return fmt.Errorf("%s: %s", label, reason)
		}
	}

	if err := r.maybeLaunchOpenPRBackpressure(ctx, label, w.Ref.repoSlug(), c, w); err != nil {
		return err
	}
	if !c.Bool("print") {
		if err := r.enforceEngineerContainerLimit(ctx, label, c.Bool("override-capacity")); err != nil {
			return err
		}
	}

	// A detached run leaves its assets for the next sweep (it cannot delete the
	// still-mounted dir on return), so the cleanup hook is discarded.
	assetsDir, _, err := writeContainerAssets(ctx, r, c.String("ward-source"), strings.TrimSpace(c.String("ward-version")))
	if err != nil {
		return err
	}

	plan, err := buildAgentPlan(c, mode, ref, w.Branch, seed, assetsDir)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	writef(os.Stderr, "%s: launch plan ready for %s (container=%s branch=%s readOnly=%t tailnet=%t/%t)\n",
		label, ref, plan.Name, plan.Branch, c.Bool("detach"), plan.HostNet, plan.TSSidecar)

	if c.Bool("print") {
		return printAgentPlan(c, plan, ref, title, seed, surface)
	}

	// Echo the issue title so the operator sees *what* this run carries, not just the
	// opaque ref number - the one line saying the right issue is in flight (ward#307).
	if line := carryingLine(label, ref, title); line != "" {
		writeln(os.Stderr, line)
	}
	// Dump the seed for a killed run's auditable task record (ward#400), unless
	// --quiet-seed (director auto-dispatch shares this console; ward#519).
	maybeDumpSeed(os.Stderr, seed, c.Bool("quiet-seed"))

	// Reserve the issue so another run won't redo it. Fold the dynamic seed context into
	// the comment so a pre-launch-gate death self-documents on the thread (ward#609).
	seedCtx := buildReservationSeedContext(w, plan, time.Now().UTC())
	var release func()
	if err := r.withAgentReservationLock(ref, func() error {
		var reserveErr error
		release, reserveErr = r.reserveIssue(ctx, label, mode, ref, plan.Name, plan.Branch, justification, seedCtx, overrideReservation(c), plan.SkipPreflight)
		return reserveErr
	}); err != nil {
		return err
	}
	// Arm a rollback: a launch that fails before the container is confirmed up must retract
	// BOTH reservation halves, not orphan the hold + forge road-block (ward#570, docs).
	launched := false
	defer func() {
		if !launched {
			release()
		}
	}()

	// Ready the ward-tailnet network before the sweep + pull burn, so a host missing it
	// gets it created here (idempotent), not a raw 125 mid-launch (ward#597).
	if plan.SkipPreflight {
		logLaunchPreflightSkips(label, plan, c.Bool("no-pull"))
	} else {
		if err := r.preflightTailnet(ctx, plan); err != nil {
			return err
		}
	}

	// Reclaim dead containers' writable layers before adding one more, so the
	// agent fleet can't exhaust the docker disk and wedge new launches (ward#272).
	r.sweepStaleContainers(ctx)

	// The engineer name is deterministic, so an exited same-issue corpse in the
	// keep-N window would block the name; clear it for reuse (ward#364).
	r.clearExitedContainer(ctx, plan.Name)
	switch {
	case plan.SkipPreflight && !c.Bool("no-pull"):
		logLaunchImageDecision(label, plan, c.Bool("no-pull"))
	case !plan.SkipPreflight && !c.Bool("no-pull"):
		logLaunchImageDecision(label, plan, c.Bool("no-pull"))
		r.pullAgentImage(ctx, plan, label)
	default:
		writef(os.Stderr, "%s: image pull skipped for %s (--no-pull)\n", label, plan.Image)
	}
	// Resolve host creds (agent + aws export-inject) before the env-file; a good AWS export
	// drops the ~/.aws mount for injected AWS_* env (ward#586).
	launchCreds := r.resolveLaunchCreds(ctx, &plan, mode)
	envFile, cleanupEnv, err := r.writeTokenEnvFile(ctx, planDispatchTarget(plan), plan.Forge, launchCreds)
	if err != nil {
		return err
	}
	writef(os.Stderr, "%s: wrote launch env file for %s\n", label, ref)
	defer cleanupEnv()
	if err := r.createAgentContainer(ctx, plan, envFile); err != nil {
		return err
	}
	// The container is up: disarm the reservation rollback so it now lives for the
	// container's lifetime (ward#570).
	launched = true
	// Spawn the detached drain-on-exit waiter so the run drains the moment it exits,
	// not only at keep-10 eviction (ward#510; docs/agent-observability.md).
	if !inContainer() {
		// In-container dispatch skips it - the waiter would die with its own reaped
		// container - and leans on the next sweep's idempotent drain instead.
		r.spawnDrainWaiter(plan.Name)
	}
	return nil
}

// prelaunchDispatch runs the shared pre-`docker create` steps for the advisor/director
// paths: the ward-tailnet ready-up (create-if-absent; ward#597), the sweep, the pull.
func (r *Runner) prelaunchDispatch(ctx context.Context, c *cli.Command, plan upPlan, label string) error {
	if err := r.preflightTailnet(ctx, plan); err != nil {
		return err
	}
	r.sweepStaleContainers(ctx)
	if !c.Bool("no-pull") {
		r.pullAgentImage(ctx, plan, label)
	}
	return nil
}

// pullHeartbeatDefault is how often a silenced detached pull beats a "still
// pulling" line so a stall on a slow/mid-push registry stays attributable.
const pullHeartbeatDefault = 30 * time.Second

// pullAgentImage pulls plan.Image: interactive streams docker, detached silences
// it but names the pull + beats a heartbeat (ward#306, ward#322; docs/agent-flags.md).
func (r *Runner) pullAgentImage(ctx context.Context, plan upPlan, label string) {
	var perr error
	if plan.Interactive {
		perr = r.dockerExec(ctx, "pull", plan.Image)
	} else {
		// Capture the live stderr before runDockerSilenced swaps it for
		// io.Discard; the named line and heartbeat must outlive the silencing.
		w := r.Runner.Stderr
		writef(w, "%s: pulling %s (silenced; this can stall on a mid-push registry)\n", label, plan.Image)
		stop := r.beatPullHeartbeat(w, label, plan.Image)
		perr = r.runDockerSilenced(ctx, true, "pull", plan.Image)
		stop()
	}
	if perr != nil {
		writef(os.Stderr, "%s: image pull failed (%v); trying the local image\n", label, perr)
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
				writef(w, "%s: still pulling %s (%s elapsed; a mid-push registry can be slow)\n",
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
	// Fail fast at this shared launch chokepoint when docker is snap-confined and
	// cannot reach ward's staged host paths, not at a raw exit-125 (ward#557, doc).
	if bin := snapDockerBin(exec.LookPath); bin != "" {
		return fmt.Errorf("%s", snapDockerRemediation(bin))
	}
	// --host-net only carries the tailnet on a host that is itself a tailnet node;
	// warn loudly when it won't, so a no-op route doesn't read as success (ward#332).
	r.maybeWarnHostNet(plan)
	// The aws capability binds ~/.aws, but a host with no AWS identity mounts an empty
	// dir - warn loudly so a NoCredentials hole doesn't read as delivered creds (ward#579).
	r.maybeWarnAWSMount(plan)
	// Seed any external (non-Forgejo) catalog.dependsOn mirror host-side before the
	// sealed container clones from the warm gitcache (ward#612).
	r.seedExternalContextMirrors(ctx, plan)
	// The ward-tailnet network ready-up (create-if-absent + standing mac-proxy box
	// warning) already ran before the pull in each dispatch path, so nothing here.
	if plan.Interactive {
		return r.dockerExec(ctx, dockerCreateArgv(plan, envFile)...)
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
		if cerr := r.dockerExec(ctx, "cp", m.Source+"/.", id+":"+m.Target); cerr != nil {
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
	out, err := r.dockerCapture(ctx, argv...)
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
	return r.dockerExec(ctx, argv...)
}

// dockerExec / dockerCapture are the choke points for every docker call: they
// suspend cli-guard's brew jail, which breaks a snap-provided docker (ward#540).
func (r *Runner) dockerExec(ctx context.Context, argv ...string) error {
	defer r.suspendSandbox()()
	return r.Runner.Exec(ctx, "docker", argv...)
}

func (r *Runner) dockerCapture(ctx context.Context, argv ...string) ([]byte, error) {
	defer r.suspendSandbox()()
	return r.Runner.Capture(ctx, "docker", argv...)
}

// suspendSandbox nils the Runner's sandbox for one docker call, restoring it via
// the returned func (safe under runDockerSilenced's sequential-launch invariant).
func (r *Runner) suspendSandbox() func() {
	saved := r.Runner.Sandbox
	r.Runner.Sandbox = nil
	return func() { r.Runner.Sandbox = saved }
}

// taskInstructions reads the DIRECT-mode task body from --instructions-file, the only
// source now the inline --instructions flag is retired (ward#362).
func taskInstructions(c *cli.Command) (string, error) {
	file := strings.TrimSpace(c.String("instructions-file"))
	if file == "" {
		return "", fmt.Errorf("no task given: pass the task as the freeform positional, or an explicit owner/repo with --instructions-file <path>")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read --instructions-file %q: %w", file, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("--instructions-file %q is empty", file)
	}
	return s, nil
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
	route, repoArg, err := classifyTaskInvocation(c.Args().First(), c.String("instructions-file"))
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if route {
		return r.runAgentTaskRoute(ctx, c, mode, strings.TrimSpace(c.Args().First()))
	}
	return r.runAgentTaskDirect(ctx, c, mode, repoArg)
}

// runAgentTaskDirect resolves the repo, files an issue from --instructions-file, and
// runs the headless container - today's behavior, unchanged. See docs.
func (r *Runner) runAgentTaskDirect(ctx context.Context, c *cli.Command, mode containerMode, repoArg string) error {
	label := agentCmdline(mode, "engineer")
	repo, _, err := r.resolveTarget(ctx, repoArg)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Same trust gate as the engineer: the container runs bypassPermissions, so
	// only file + work against an owner in the trusted-owner set.
	if !r.ownerAllowed(repo.Owner) {
		return r.untrustedOwnerErr(label, repo.Owner)
	}
	instructions, err := taskInstructions(c)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// Validate --workflow before filing an issue so a typo doesn't leave a dangling
	// ticket behind an unparseable flag (ward#508).
	wf, werr := agentWorkflow(c, repo.slug())
	if werr != nil {
		return fmt.Errorf("%s: %w", label, werr)
	}
	title := taskTitle(instructions)
	body := taskBody(mode, instructions)

	if c.Bool("print") {
		return printAgentTaskPlan(c, mode, repo, title, body)
	}

	// task always detaches, so host dispatch is the last interactive moment - surface
	// a stale-ward reminder before it files+launches (ward#143).
	r.maybeWarnWardOutdatedForTask(ctx, c, label)

	cl, err := r.hostTrackerClient(ctx, trackerForgejo, mode)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	number, err := cl.createIssue(ctx, repo.Owner, repo.Name, title, body)
	if err != nil {
		return fmt.Errorf("%s: file issue in %s/%s: %w", label, repo.Owner, repo.Name, err)
	}
	ref := agentIssueRef{Owner: repo.Owner, Repo: repo.Name, Number: number, Tracker: trackerForgejo}
	writef(os.Stderr, "%s: filed %s - %s\n", label, ref, ref.url())

	// The freeform engineer run carries headless, so it gets the same pre-flight
	// (ward#149): a NO-GO comments on the just-filed issue and launches nothing.
	var justification string
	if preflightWanted(c) {
		proceed, read, perr := r.runPreflight(ctx, mode, "engineer", resolvedWork{Ref: ref, Title: title, Body: body})
		if perr != nil {
			return fmt.Errorf("%s: pre-flight: %w", label, perr)
		}
		if !proceed {
			// runPreflight already reported the NO-GO and posted the issue comment.
			return nil
		}
		// On a GO, carry the read into the reservation comment (ward#383).
		justification = read
	}

	if forwarded, ferr := r.forwardFreeformEngineerLaunchToHostBroker(ctx, c, mode, ref); forwarded {
		return ferr
	}

	// The freeform instructions are the filed body (no --details); a headless seed
	// (inlined body + reflection) carried under the resolved workflow (#167, #281, #508).
	reviewGate := reviewGateWanted(c, mode, ref)
	seed := agentSeedPromptWorkflow(ref, title, body, "", true, nil, wf, reviewGate, "")
	seed += agentRunBudgetNote(roleEngineer)
	return r.launchAgentContainer(ctx, c, mode, "engineer",
		resolvedWork{Ref: ref, Title: title, Body: body, Workflow: wf, ReviewGate: reviewGate, Seed: seed}, justification)
}

// maybeWarnWardOutdatedForTask keeps the freeform engineer reminder branch out of
// runAgentTaskDirect so the launch path stays under the repo's cyclomatic limit.
func (r *Runner) maybeWarnWardOutdatedForTask(ctx context.Context, c *cli.Command, label string) {
	if preflightSkipped(c) {
		writef(os.Stderr, "%s: skipping ward update reminder (--skip-preflight)\n", label)
		return
	}
	r.maybeWarnWardOutdated(ctx)
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
	// --print skips the workflow validation gate above (it never files), so a bad
	// value simply previews as the default rather than erroring here (ward#508).
	wf, _ := agentWorkflow(c, repo.slug())
	reviewGate := reviewGateWanted(c, mode, previewRef)
	seed := agentSeedPromptWorkflow(previewRef, title, body, "", true, nil, wf, reviewGate, "")
	seed += agentRunBudgetNote(roleEngineer)
	plan, err := buildUpPlan(c, repo, mode, roleEngineer, "", "", []string{seed}, false)
	if err != nil {
		return err
	}
	plan.Headless = true
	plan.Interactive = false
	plan.TTY = false
	plan.Role = roleEngineer
	plan.Workflow = wf
	if plan.Branch == "" {
		plan.Branch = "issue-<N>"
	}
	// Mirror the live engineer name shape; the real issue number lands once filed,
	// so the placeholder reads engineer-<driver>-<repo>-<N> with <N> unresolved.
	plan.Name = fmt.Sprintf("%s-%s-%s-<N>", roleEngineer, mode, safeRepoName(repo))

	var b strings.Builder
	writef(&b, "# %s (print)\n", agentCmdline(mode, "engineer"))
	writef(&b, "headless: agent runs detached in print mode (-p)\n")
	writef(&b, "repo:    %s\n", repo.slug())
	writef(&b, "branch:  %s\n", plan.Branch)
	writef(&b, "workflow: %s\n", plan.Workflow.orDefault())
	writef(&b, "name:    %s\n", plan.Name)
	if plan.Mode == modeOpencode {
		if endpoint := opencodeEndpointPreview(plan); endpoint != "" {
			writef(&b, "agent-proxy: %s\n", endpoint)
		}
	}
	writef(&b, "correlation:\n")
	for _, line := range printCorrelationEnvelope(plan) {
		writef(&b, "  %s\n", line)
	}
	writef(&b, "----- issue to file -----\ntitle: %s\n\n%s\n----- end -----\n", title, body)
	writef(&b, "----- seeded prompt (#N filled once filed) -----\n%s\n----- end -----\n", seed)
	appendLaunchPreflightNotes(&b, plan, c.Bool("no-pull"))
	switch {
	case plan.SkipPreflight:
	case c.Bool("no-pull"):
		writef(&b, "# pull skipped (--no-pull); image: %s\n", plan.Image)
	default:
		writef(&b, "docker pull %s\n", plan.Image)
	}
	writef(&b, "docker %s\n", strings.Join(dockerCreateArgv(plan, "<ward-forgejo-token-envfile>"), " "))
	_, werr := io.WriteString(out, b.String())
	return werr
}

func opencodeEndpointPreview(p upPlan) string {
	if v := strings.TrimSpace(p.ConfigEnv["WARD_OLLAMA_URL"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("WARD_OLLAMA_URL")); v != "" {
		return v
	}
	if a, ok := frontierAgentDefaults[string(modeOpencode)]; ok {
		return a.Endpoint
	}
	return ""
}

func printCorrelationEnvelope(p upPlan) []string {
	env := p.correlationEnv()
	keys := []string{
		"WARD_RUN_ID",
		"WARD_CONTAINER_NAME",
		"WARD_ROLE",
		"WARD_HARNESS",
		"WARD_TARGET_OWNER",
		"WARD_TARGET_NAME",
		"WARD_TARGET_REPO",
		"WARD_ISSUE_REF",
		"WARD_CONTEXT_LEVEL",
		"WARD_VERSION",
		"WARD_THREAD_ID",
	}
	var out []string
	for _, key := range keys {
		if v := strings.TrimSpace(env[key]); v != "" {
			out = append(out, key+"="+v)
		}
	}
	if wf := strings.TrimSpace(string(p.Workflow.orDefault())); wf != "" && !p.Workflow.landsOnMain() {
		out = append(out, "WARD_WORKFLOW="+wf)
	}
	return out
}

// ownerAllowed reports whether owner is in ward's trusted-owner set, via
// cli-guard's pkg/ownertrust (ward supplies the accepted set).
func (r *Runner) ownerAllowed(owner string) bool {
	owners := r.trustedOwners()
	return ownertrust.List{Primary: firstTrustedOwner(owners), Extra: trustedOwnerExtras(owners)}.Allowed(owner)
}

// untrustedOwnerErr is the trust-gate refusal shared by every dispatch surface:
// it names the accepted set and points at docs/agent-trust-gate.md (ward#484).
func (r *Runner) untrustedOwnerErr(label, owner string) error {
	return exitcode.New(dispatchUntrustedOwner, "untrusted_owner",
		fmt.Errorf("%s: refusing untrusted owner %q (allowed: %s). This build dispatches only for its configured trusted owners - see docs/agent-trust-gate.md",
			label, owner, strings.Join(r.trustedOwners(), ", ")), "")
}

// printAgentPlan renders the resolved issue, the seeded prompt, and the docker
// plan without firing - the dry-run preview, safe with no docker daemon.
func printAgentPlan(c *cli.Command, p upPlan, ref agentIssueRef, title, seed, surface string) error {
	out := c.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	var b strings.Builder
	writef(&b, "# %s (print)\n", agentCmdline(p.Mode, surface))
	if p.Headless {
		writef(&b, "headless: agent runs detached in print mode (-p)\n")
	}
	writef(&b, "issue:   %s\n", ref)
	writef(&b, "url:     %s\n", ref.url())
	writef(&b, "title:   %s\n", title)
	writef(&b, "repo:    %s\n", p.Repo.slug())
	writef(&b, "branch:  %s\n", p.Branch)
	writef(&b, "workflow: %s\n", p.Workflow.orDefault())
	writef(&b, "name:    %s\n", p.Name)
	if p.Mode == modeOpencode {
		if endpoint := opencodeEndpointPreview(p); endpoint != "" {
			writef(&b, "agent-proxy: %s\n", endpoint)
		}
	}
	writef(&b, "correlation:\n")
	for _, line := range printCorrelationEnvelope(p) {
		writef(&b, "  %s\n", line)
	}
	writef(&b, "%s", seedLogBlock(seed))
	appendLaunchPreflightNotes(&b, p, c.Bool("no-pull"))
	switch {
	case p.SkipPreflight:
	case c.Bool("no-pull"):
		writef(&b, "# pull skipped (--no-pull); image: %s\n", p.Image)
	default:
		writef(&b, "docker pull %s\n", p.Image)
	}
	if p.TSSidecar {
		// The run attaches to the standing mac-proxy box over ward-tailnet (shown in
		// the run argv's --network below); ward preflights the box, never starts it (ward#349).
		writef(&b, "# preflight: docker %s (mac-proxy must be attached)\n", strings.Join(dockerTailnetInspectArgv(), " "))
	}
	writef(&b, "docker %s\n", strings.Join(dockerCreateArgv(p, "<ward-forgejo-token-envfile>"), " "))
	_, err := io.WriteString(out, b.String())
	return err
}
