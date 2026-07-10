package main

// container_reap_compute.go holds the pure decision logic behind `ward
// container reap` (side effects live in container_reap.go). See docs/container-reap.md.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/scan"
)

// reapAction is what the reaper does with residual work after the agent exits,
// decided by static code with the agent's permissions out of the loop.
type reapAction int

const (
	// reapNothing: tree clean and HEAD already on canonical main.
	reapNothing reapAction = iota
	// reapPushMain: residual work integrates cleanly and the scan is clean.
	reapPushMain
	// reapSalvage: conflict, flagged diff, or rejected push - preserve on a branch.
	reapSalvage
)

// reapReason names why a salvage happened, surfaced in the forgejo issue so the
// operator knows whether to merge, clean up, or investigate.
type reapReason string

const (
	reasonConflict reapReason = "merge conflict integrating onto main"
	reasonScan     reapReason = "diff flagged by the junk scan"
	reasonCloseRef reapReason = "missing same-repo closing reference"
	reasonPushRace reapReason = "push to main was rejected (the remote advanced)"
	reasonPushFail reapReason = "push to main failed"
	reasonAuthFail reapReason = "push to main was rejected on auth (dead or rotated PAT)"
)

const (
	commitStateAgentDidNotCommit                  = "agent did not commit"
	commitStateCommitExistedButLackedCloseTrailer = "commit existed but lacked close trailer"
)

// authFailureMarkers are substrings git/forgejo emit when a push is rejected on
// credentials not content; matched case-insensitively against the push output.
var authFailureMarkers = []string{
	"authentication failed",
	"invalid credentials",
	"invalid username or password",
	"could not read username",
	"could not read password",
	"403 forbidden",
	"401 unauthorized",
	"http 403",
	"http 401",
	"remote: forbidden",
	"remote: unauthorized",
	"permission denied",
	"access denied",
}

// isAuthFailure reports whether git push output names a credential rejection
// (the container's baked PAT went dead mid-run) rather than a content/race reject.
func isAuthFailure(pushOutput string) bool {
	o := strings.ToLower(pushOutput)
	for _, m := range authFailureMarkers {
		if strings.Contains(o, m) {
			return true
		}
	}
	return false
}

// reapInputs are the facts the reaper gathers from git + the scan before it
// decides; a pure function of these keeps the policy testable.
type reapInputs struct {
	// HasResidualWork: worktree dirty or HEAD ahead of canonical origin/main.
	HasResidualWork bool
	// IntegrationClean: residual work rebased onto origin/main without conflict.
	IntegrationClean bool
	// Findings are junk-scan hits on the residual diff; non-empty -> salvage.
	Findings []scan.Finding
}

// decideReap is the whole policy: clean tree -> nothing; clean integration +
// clean scan -> main; anything else -> salvage (non-destructive, the safe default).
func decideReap(in reapInputs) reapAction {
	if !in.HasResidualWork {
		return reapNothing
	}
	if in.IntegrationClean && len(in.Findings) == 0 {
		return reapPushMain
	}
	return reapSalvage
}

// formatTokenAge renders the container's age at reap time from its RFC3339 start
// stamp and now; reports false on a missing, unparseable, or future stamp (ward#103).
func formatTokenAge(upAt string, now time.Time) (string, bool) {
	s := strings.TrimSpace(upAt)
	if s == "" {
		return "", false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", false
	}
	d := now.Sub(t)
	if d < 0 {
		return "", false
	}
	return humanDuration(d), true
}

// humanDuration renders a duration as a compact age string (e.g. "3h42m",
// "2d3h", "45s") for the salvage issue. Only the two most significant units show.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	mins := int((d % time.Hour) / time.Minute)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// --- reap diagnostics block (ward#531) ---------------------------------------

// reapDecision is the decision-branch context a salvage/fail site hands the
// diagnostics gatherer: which gate fired plus the provenance/landed verdict (ward#531).
type reapDecision struct {
	// Gate names the decision branch that tripped (e.g. "merge conflict integrating
	// onto main", "no run-owned landed commit after dispatch").
	Gate string
	// ProvState is the provenance file's state at the decision: "present",
	// "missing or unreadable", or "not read (no origin/main)".
	ProvState string
	// CommitState distinguishes dirty-only residual work from already-committed work.
	CommitState string
	// Landed is the run-owned-landed verdict (runProvenanceLanded) the gate saw.
	Landed bool
}

// reapDiagnostics is the debugging block the reaper dumps on salvage/failure
// (ward#531): the facts a false-salvage post-mortem needs. See docs/container-reap.md.
type reapDiagnostics struct {
	// WardVersion is the ward release running the reaper (the compiled Version).
	WardVersion string
	// VersionSource is how that version resolved: a WARD_VERSION/--ward-version pin
	// or releases/latest resolved in-container.
	VersionSource string
	// Head / OriginMain are the short shas at reap; OriginMain is "" when absent.
	Head       string
	OriginMain string
	// HeadOnMain is `git merge-base --is-ancestor HEAD origin/main`: true means HEAD
	// is already contained in origin/main, the exact false-salvage signature.
	HeadOnMain bool
	// Gate / Reason name the decision branch taken and its human reason.
	Gate   string
	Reason reapReason
	// ProvState / CommitState / Landed mirror reapDecision: provenance presence,
	// whether the agent had already committed, and the landed proof.
	ProvState   string
	CommitState string
	Landed      bool
	// Status is the `git status --porcelain` snapshot; TokenAge is the container
	// uptime at reap (a baked-PAT age proxy).
	Status   string
	TokenAge string
}

const (
	reapDiagHeader = "--- reap diagnostics ---"
	reapDiagFooter = "--- end reap diagnostics ---"
)

// renderReapDiagnostics renders the one clearly-delimited block (ward#531): a grep
// anchor plus aligned facts, readable in a log and foldable into the issue body.
func renderReapDiagnostics(d reapDiagnostics) string {
	var b strings.Builder
	b.WriteString(reapDiagHeader + "\n")
	fmt.Fprintf(&b, "ward version:      %s\n", d.WardVersion)
	fmt.Fprintf(&b, "version source:    %s\n", d.VersionSource)
	fmt.Fprintf(&b, "HEAD:              %s\n", shaOrDash(d.Head))
	fmt.Fprintf(&b, "origin/main:       %s\n", shaOrDash(d.OriginMain))
	fmt.Fprintf(&b, "ancestry:          %s\n", ancestryVerdict(d))
	fmt.Fprintf(&b, "decision gate:     %s\n", d.Gate)
	fmt.Fprintf(&b, "reason:            %s\n", d.Reason)
	fmt.Fprintf(&b, "provenance:        %s\n", d.ProvState)
	if d.CommitState != "" {
		fmt.Fprintf(&b, "agent commit:      %s\n", d.CommitState)
	}
	fmt.Fprintf(&b, "run-owned landed:  %s\n", yesNo(d.Landed))
	fmt.Fprintf(&b, "working tree:      %s\n", treeSummary(d.Status))
	if d.TokenAge != "" {
		fmt.Fprintf(&b, "container uptime:  %s (baked Forgejo PAT age proxy)\n", d.TokenAge)
	}
	b.WriteString(reapDiagFooter)
	return b.String()
}

// ancestryVerdict renders the HEAD-vs-origin/main relationship in plain words, so
// the false-salvage case (HEAD already on main) reads as an alarm, not a sha diff.
func ancestryVerdict(d reapDiagnostics) string {
	switch {
	case d.OriginMain == "":
		return "origin/main absent - cannot compute ancestry"
	case d.HeadOnMain:
		return "HEAD is ALREADY on origin/main - a salvage here is a FALSE salvage (ward#504 signature)"
	default:
		return "HEAD is NOT yet on origin/main - residual work remains to land"
	}
}

// treeSummary collapses a porcelain status to a one-line count; the full snapshot
// still ships in its own issue section.
func treeSummary(status string) string {
	s := strings.TrimSpace(status)
	if s == "" {
		return "clean"
	}
	return fmt.Sprintf("%d dirty path(s)", len(strings.Split(s, "\n")))
}

// shaOrDash renders a sha or a dash placeholder when git could not resolve it.
func shaOrDash(sha string) string {
	if strings.TrimSpace(sha) == "" {
		return "-"
	}
	return sha
}

// yesNo renders a bool as a word for the aligned block.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// --- salvage branch + issue rendering ----------------------------------------

// salvageBranchPrefix namespaces every reaper-pushed branch so they are easy to
// list and reap later, and never collide with a feature branch.
const salvageBranchPrefix = "ward-salvage/"

// salvageIssueTitlePrefix marks reaper-filed issues so a later run can find an
// open one for the same repo and append to it instead of filing a duplicate.
const salvageIssueTitlePrefix = "[ward-salvage]"

// salvageBranchName builds the branch the reaper pushes residual work to.
func salvageBranchName(id string) string {
	return salvageBranchPrefix + id
}

// salvageReport is everything the issue text needs about one cleanup salvage.
type salvageReport struct {
	Repo        targetRepo
	Mode        string
	Branch      string
	Reason      reapReason
	CommitState string
	Findings    []scan.Finding
	// Issue is the carried issue this run was dispatched for (0 for a freeform
	// run); a carried salvage comments here instead of filing a new issue (ward#518).
	Issue int
	// AuthCause is set when the salvage was triggered by a credential-rejected
	// push (a dead/rotated PAT), not a content conflict or race (ward#103).
	AuthCause bool
	// TokenAge is the container's age at reap time (e.g. "3h42m"), a proxy for how
	// stale the baked PAT is; empty when the start time is unknown.
	TokenAge string
	// Status is the `git status --porcelain` snapshot at reap time, for context.
	Status string
	// Base is the forgejo base URL, used to render the fetch/recover commands.
	Base string
	// Diagnostics is the ward#531 block folded into the issue body so the same facts
	// survive on the durable notification, not only on ephemeral stderr.
	Diagnostics reapDiagnostics
	// PullRequestURL is the salvage PR opened for this branch when PRs are available.
	PullRequestURL string
	// PullRequestUnavailable names why ward fell back to branch-only salvage.
	PullRequestUnavailable string
}

// salvageIssueTitle is stable per repo+branch so duplicate detection works.
func salvageIssueTitle(r salvageReport) string {
	return fmt.Sprintf("%s %s: unmerged container work on %s",
		salvageIssueTitlePrefix, r.Repo.Name, r.Branch)
}

// salvageIssueBody renders the standalone operator-facing issue for a freeform
// run (no carried issue): intro plus the shared detail body.
func salvageIssueBody(r salvageReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "An ephemeral `ward container` (%s mode) finished but its work was **not merged to `main`**, so cleanup preserved it on a branch before the container was torn down.\n\n", r.Mode)
	b.WriteString(salvageDetailBody(r))
	return b.String()
}

// salvageCommentBody renders the salvage notice as a comment on the carried
// issue (ward#518): a reopen banner plus the shared detail body.
func salvageCommentBody(r salvageReport) string {
	var b strings.Builder
	visible := "WARD-REAP: reopened 🛑"
	fmt.Fprintf(&b, "An ephemeral `ward container` (%s mode) dispatched for this issue finished but its work was **not merged to `main`**, so cleanup preserved it on a branch before teardown and reopened the issue (a closing reference for #%d never reached `main`). Recover from the salvage branch below.\n\n", r.Mode, r.Issue)
	b.WriteString(salvageDetailBody(r))
	return collapsedIssueComment(visible, "salvage details", b.String())
}

// salvageDetailBody is the shared body of both the standalone issue and the
// carried-issue comment: facts, likely-cause, recovery, findings, tree snapshot.
func salvageDetailBody(r salvageReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- **Repo:** `%s`\n", r.Repo.slug())
	fmt.Fprintf(&b, "- **Salvage branch:** `%s`\n", r.Branch)
	if r.PullRequestURL != "" {
		fmt.Fprintf(&b, "- **Pull request:** %s\n", r.PullRequestURL)
	} else if r.PullRequestUnavailable != "" {
		fmt.Fprintf(&b, "- **Pull request:** not opened - %s\n", r.PullRequestUnavailable)
	}
	fmt.Fprintf(&b, "- **Reason:** %s\n", r.Reason)
	if r.CommitState != "" {
		fmt.Fprintf(&b, "- **Agent commit:** %s\n", r.CommitState)
	}
	if r.TokenAge != "" {
		fmt.Fprintf(&b, "- **Container uptime at reap:** %s (age of the baked Forgejo PAT snapshot; a long-lived container is likelier to carry a rotated token)\n", r.TokenAge)
	}
	b.WriteString("\n")

	if r.AuthCause {
		b.WriteString("## Likely cause: dead/rotated PAT, not a conflict\n\n")
		b.WriteString("The push was rejected on **credentials**, not content. The Forgejo PAT baked into this container at `up` time was most likely rotated or revoked while it ran, so the final push to `main` (and any salvage-branch push) failed on auth. This is **not** a merge conflict - the work on the salvage branch should rebase and land cleanly once pushed with a live token. Don't rotate the PAT while containers are in flight; see docs/container-reap.md.\n\n")
	}

	// Fold the ward#531 diagnostics block in verbatim so a false-salvage
	// self-diagnoses on the durable issue, not only on ephemeral stderr.
	if strings.TrimSpace(r.Diagnostics.WardVersion) != "" {
		b.WriteString("## Cleanup diagnostics\n\n```\n")
		b.WriteString(renderReapDiagnostics(r.Diagnostics))
		b.WriteString("\n```\n\n")
	}

	b.WriteString(closingReferenceStateBody(r))

	b.WriteString("## Recover\n\n```bash\n")
	fmt.Fprintf(&b, "git fetch %s %s\n", r.Repo.cloneURL(r.Base), r.Branch)
	fmt.Fprintf(&b, "git checkout -b %s FETCH_HEAD\n", r.Branch)
	b.WriteString("```\n\n")

	if r.Reason == reasonCloseRef && r.Issue != 0 {
		b.WriteString("This salvage was blocked by a missing closing reference. To recover, amend or cherry-pick the salvaged work so the landing commit message includes `closes #")
		fmt.Fprintf(&b, "%d`, or add a small empty trailer commit with `closes #%d`, then land the branch.\n\n", r.Issue, r.Issue)
	}

	if len(r.Findings) > 0 {
		b.WriteString("## Junk-scan findings\n\nThese paths kept the diff off `main`. Review before merging:\n\n")
		for _, f := range sortedFindings(r.Findings) {
			fmt.Fprintf(&b, "- `%s` - %s\n", f.Path, f.Reason)
		}
		b.WriteString("\n")
	}

	if strings.TrimSpace(r.Status) != "" {
		b.WriteString("## Working tree at reap time\n\n```\n")
		b.WriteString(strings.TrimRight(r.Status, "\n"))
		b.WriteString("\n```\n")
	}
	return b.String()
}

func closingReferenceStateBody(r salvageReport) string {
	if r.Reason != reasonCloseRef || r.CommitState == "" {
		return ""
	}
	switch r.CommitState {
	case commitStateAgentDidNotCommit:
		return "## Closing reference state\n\nThe agent did not commit before teardown. The reaper had to snapshot dirty files into a residual commit, but that snapshot still lacked the same-repo closing reference needed to land.\n\n"
	case commitStateCommitExistedButLackedCloseTrailer:
		return "## Closing reference state\n\nA commit already existed before teardown, but the commit that should have landed did not carry the same-repo closing reference.\n\n"
	default:
		return "## Closing reference state\n\n" + r.CommitState + "\n\n"
	}
}

// --- granted-repo (--repo) push verification (ward#291) ----------------------

// containerWorkspace is where the entrypoint clones the target and every --repo
// grant, as /workspace/<name>; mirrors cloneExtraRepo's layout (ward#230).
const containerWorkspace = "/workspace"

// extraRepoWorkDir is the in-container working copy of a granted repo, the tree
// the reaper verifies actually landed before the run reads as done.
func extraRepoWorkDir(repo targetRepo) string {
	return containerWorkspace + "/" + repo.Name
}

// extraRepoUnlanded is one granted repo the reaper could not confirm landed on its
// remote main: the verification verdict plus how its work was preserved (ward#291).
type extraRepoUnlanded struct {
	Repo targetRepo
	// Branch is the salvage branch the un-landed work was pushed to, empty when the
	// salvage-branch push itself failed (work is then only in the container log).
	Branch string
	// Ahead is the count of local commits not on the freshly-fetched remote main.
	Ahead int
	// Status is the `git status --porcelain` snapshot of the granted clone at reap.
	Status string
	// NoMain marks a granted repo whose remote had no main to verify against, so the
	// reaper could not prove the work landed and treated it as un-landed.
	NoMain bool
	// PushErr is the salvage-branch push error, set when even preservation failed.
	PushErr string
}

// unlandedExtraReposComment renders the reaper's comment for the reopened issue:
// which grants did not land, where each was preserved, and how to recover (ward#291).
func unlandedExtraReposComment(env reapEnv, reports []extraRepoUnlanded) string {
	var b strings.Builder
	visible := "WARD-REAP: reopened 🛑"
	fmt.Fprintf(&b, "This run held `--repo` grants and closed against `%s`, but cleanup could not confirm "+
		"every granted repo's work reached its `main`. A secondary push can be silently rejected (a "+
		"non-fast-forward on a busy `main`, a dead/rotated PAT) while the primary push succeeds, so the "+
		"issue is **reopened** rather than left reading \"done\" with the cross-repo half lost.\n\n",
		env.repo().slug())
	for _, rep := range sortedUnlanded(reports) {
		fmt.Fprintf(&b, "### `%s`\n\n", rep.Repo.slug())
		if rep.NoMain {
			b.WriteString("- **Verdict:** could not verify - the remote had no `main` branch to compare against.\n")
		} else {
			fmt.Fprintf(&b, "- **Verdict:** %d local commit(s) never reached `origin/main`.\n", rep.Ahead)
		}
		switch {
		case rep.Branch != "":
			fmt.Fprintf(&b, "- **Preserved on:** `%s`\n\n", rep.Branch)
			b.WriteString("```bash\n")
			fmt.Fprintf(&b, "git fetch %s %s\n", rep.Repo.cloneURL(env.Base), rep.Branch)
			fmt.Fprintf(&b, "git checkout -b %s FETCH_HEAD\n", rep.Branch)
			b.WriteString("```\n")
		default:
			b.WriteString("- **Preserved on:** none - the salvage-branch push also failed")
			if rep.PushErr != "" {
				fmt.Fprintf(&b, " (`%s`)", firstLine(rep.PushErr))
			}
			b.WriteString("; recover the patch from this container's `docker logs` before teardown.\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Re-run the cross-repo half, or - per ward#291 - file it as a native issue in the granted " +
		"repo so it becomes a single-repo run that sidesteps this failure mode.\n")
	return collapsedIssueComment(visible, "grant details", b.String())
}

// sortedUnlanded orders un-landed grants by slug for deterministic rendering.
func sortedUnlanded(in []extraRepoUnlanded) []extraRepoUnlanded {
	out := append([]extraRepoUnlanded(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Repo.slug() < out[j].Repo.slug() })
	return out
}

// firstLine returns the first non-empty line of s, trimmed, so a multi-line git
// error collapses to a single readable clause in the issue comment.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}

// sortedFindings returns findings ordered by path for deterministic rendering.
func sortedFindings(in []scan.Finding) []scan.Finding {
	out := append([]scan.Finding(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
