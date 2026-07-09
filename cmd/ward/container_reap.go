package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/scan"
	"github.com/urfave/cli/v3"
)

// container_reap.go is the side-effecting half of the container reaper: the
// verb the entrypoint runs on every agent exit. See docs/container-reap.md.

// reapEnv is the container-supplied context the reaper reads. All of it is set
// by the entrypoint; FORGEJO_TOKEN is the same push token git already uses.
type reapEnv struct {
	Owner string
	Name  string
	Base  string
	Mode  string
	Token string
	Forge forge
	// Container is WARD_CONTAINER_NAME, the run correlation id stamped on the reap
	// surface (ward#517). See docs/container-lifecycle-logs.md.
	Container string
	// UpAt is the container's RFC3339 start stamp (WARD_CONTAINER_UP), diffed
	// against reap time to report the baked PAT's age on a salvage (ward#103).
	UpAt string
	// Issue is the carried issue number (WARD_TARGET_ISSUE, 0 for a bare `up`); a
	// clean reap releases the reservation on it if the agent never launched (ward#264).
	Issue int
	// Launched mirrors WARD_AGENT_LAUNCHED: set once the entrypoint reaches the
	// agent launch. Unset means a pre-launch death (e.g. the ward#222 smoke test).
	Launched bool
	// ReadOnly mirrors WARD_READONLY (ward#293): a read-only explore session, so the
	// reaper skips salvage (it would otherwise push whatever the agent left behind).
	ReadOnly bool
	// ExtraRepos mirrors WARD_EXTRA_REPOS (ward#230): the --repo grants this run.
	// The reaper verifies each one landed before done (ward#291).
	ExtraRepos []targetRepo
	// Workflow mirrors WARD_WORKFLOW (ward#508).
	// Empty reads as direct-main; PR and patch-only modes stay on a branch.
	Workflow workflowMode
}

// envAgentLaunched is the entrypoint flag exported just before the agent launches,
// read by the reaper to tell a smoke-test death from a real agent run (ward#264).
const envAgentLaunched = "WARD_AGENT_LAUNCHED"

func readReapEnv() (reapEnv, error) {
	e := reapEnv{
		Owner:     os.Getenv("WARD_TARGET_OWNER"),
		Name:      os.Getenv("WARD_TARGET_NAME"),
		Base:      envOr("WARD_CLONE_BASE", os.Getenv("WARD_FORGEJO_BASE")),
		Mode:      os.Getenv("WARD_MODE"),
		Token:     os.Getenv("FORGEJO_TOKEN"),
		Forge:     parseForge(os.Getenv("WARD_FORGE")),
		UpAt:      os.Getenv("WARD_CONTAINER_UP"),
		Container: os.Getenv("WARD_CONTAINER_NAME"),
		Launched:  os.Getenv(envAgentLaunched) == "1",
		ReadOnly:  os.Getenv("WARD_READONLY") == "1",
		Workflow:  canonicalWorkflow(workflowMode(os.Getenv("WARD_WORKFLOW"))),
	}
	// A missing/garbage WARD_TARGET_ISSUE parses to 0: "no issue to release".
	e.Issue, _ = strconv.Atoi(os.Getenv("WARD_TARGET_ISSUE"))
	e.ExtraRepos = parseExtraReposEnv(os.Getenv("WARD_EXTRA_REPOS"), e.Owner, e.Name)
	if e.Owner == "" || e.Name == "" || e.Base == "" {
		return e, fmt.Errorf("ward container reap: missing WARD_TARGET_OWNER/NAME/WARD_CLONE_BASE (run inside a ward container)")
	}
	if e.Mode == "" {
		e.Mode = string(defaultAgentMode())
	}
	return e, nil
}

func (e reapEnv) repo() targetRepo { return targetRepo{Owner: e.Owner, Name: e.Name} }

// reapStartLine is the reaper's opening lifecycle marker (ward#517): the run
// correlation id (container=) first, then key=value run-shape fields.
func (e reapEnv) reapStartLine() string {
	return fmt.Sprintf("WARD-REAP: start container=%s repo=%s/%s issue=%d readOnly=%t extraRepos=%d launched=%t",
		e.Container, e.Owner, e.Name, e.Issue, e.ReadOnly, len(e.ExtraRepos), e.Launched)
}

// reapLogFlushLine is the container-visible archive contract: tell the operator
// where the durable console/log archive is expected to land, or say none is set.
func (e reapEnv) reapLogFlushLine() string {
	if strings.TrimSpace(e.Container) == "" {
		return "ward container reap: no durable log flush configured"
	}
	return fmt.Sprintf("ward container reap: logs flushed to %s", agentLogsDisplayDir(e.Container))
}

// reservationReleasable reports whether a clean reap should retract this run's
// hold: only a container that carried an issue and never launched the agent (ward#264).
func (e reapEnv) reservationReleasable() bool {
	return !e.Launched && e.Issue != 0
}

func reapBoundaryReason(w workflowMode) string {
	switch w.orDefault() {
	case workflowDirectToMain:
		return "tree clean, HEAD on origin/main"
	case workflowPullRequest:
		return "workflow pull-requests boundary reached: branch pushed, pull request open, and required CI checks are green"
	case workflowPullRequestAndMerge:
		return "workflow pull-requests-and-merge boundary reached: branch pushed, pull request open, and required CI checks are green"
	case workflowRemoteBranchOnly:
		return "workflow patch-only boundary reached: remote branch pushed"
	default:
		return "tree clean, workflow boundary reached"
	}
}

func containerReapCommand() *cli.Command {
	return &cli.Command{
		Name:   "reap",
		Hidden: true, // ward#263: entrypoint-internal, not a hand-run verb
		Usage:  "Clean up residual work before container teardown: land it on main if clean, else push a salvage branch and file a Forgejo issue.",
		Description: `reap runs once the agent exits, on every exit, as deterministic static
code. It stages and commits anything the agent left uncommitted, integrates
onto the latest main, and then: if the diff is clean and integrates, pushes
straight to main; otherwise preserves the work on a ward-salvage/<id> branch and
files (or appends to) a forgejo issue so nothing is lost when the container is
torn down. Normally invoked by the container entrypoint, not by hand.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "work", Usage: "the clone working tree to reap (default: cwd / $WARD_REAP_WORK)"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "container.reap",
				SkipPolicy: true, // the reaper operates on a dirty tree by design
				Action:     func(ctx context.Context, cmd *cli.Command) error { return r.runContainerReap(ctx, cmd) },
			}, r.Audit)(ctx, c)
		},
	}
}

// runContainerReap is the reaper's control flow: reap the target tree, then verify
// every --repo grant actually landed (ward#291) so a half-landed run can't read done.
func (r *Runner) runContainerReap(ctx context.Context, c *cli.Command) error {
	env, err := readReapEnv()
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, env.reapStartLine())
	fmt.Fprintln(os.Stderr, env.reapLogFlushLine())
	if env.ReadOnly {
		// A read-only explore session never mutates the remote (ward#293): skip
		// capture/commit/push outright, leaving the throwaway clone untouched.
		fmt.Fprintln(os.Stderr, "ward container reap: read-only session, nothing to salvage (skipping)")
		return nil
	}
	work := resolveReapWork(c)
	if !isGitWorkTree(ctx, r, work) {
		return fmt.Errorf("ward container reap: %q is not a git work tree", work)
	}
	fmt.Fprintf(os.Stderr, "ward container reap: work tree confirmed at %s\n", work)
	terr := r.reapTargetTree(ctx, work, env, true)
	r.verifyExtraReposLanded(ctx, env)
	return terr
}

// reapTargetTree is the target half of a reap, from capture through salvage.
func (r *Runner) reapTargetTree(ctx context.Context, work string, env reapEnv, releaseReservation bool) error { //nolint:gocognit,gocyclo,cyclop,nestif
	if env.ReadOnly {
		fmt.Fprintln(os.Stderr, "ward container reap: read-only session, nothing to salvage (skipping)")
		return nil
	}
	statusSnapshot := r.captureAndCommitResidual(ctx, work, env)
	fmt.Fprintf(os.Stderr, "ward container reap: residual status snapshot for %s: %q\n", work, strings.TrimSpace(statusSnapshot))

	// Refresh remote-tracking refs so we integrate against the latest main; a
	// fetch failure leaves the clone-time origin/main as a usable base.
	fmt.Fprintln(os.Stderr, "ward container reap: fetch origin start")
	_ = r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin")
	fmt.Fprintln(os.Stderr, "ward container reap: fetch origin done")
	if !refExists(ctx, r, work, "origin/main") {
		// Empty repo (no base branch): establish main from a clean run rather than
		// salvage it just for starting empty (ward#599, docs/container-reap.md).
		return r.reapEstablishMain(ctx, work, env, statusSnapshot, releaseReservation)
	}

	if !env.Workflow.landsOnMain() {
		findings := scan.Diff(r.diffEntries(ctx, work, "origin/main...HEAD"))
		if len(findings) > 0 {
			fmt.Fprintf(os.Stderr, "ward container cleanup: --workflow %s produced a diff the junk scan rejected; preserving on a salvage branch instead of treating the workflow boundary as success\n", env.Workflow.orDefault())
			return r.salvage(ctx, work, env, reasonScan, false, findings, statusSnapshot,
				reapDecision{Gate: "junk scan flagged the workflow-boundary diff", ProvState: "not read (workflow hold)"})
		}
		fmt.Fprintf(os.Stderr, "WARD-REAP: nothing to reap (%s)\n", reapBoundaryReason(env.Workflow))
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}

	// Nothing to reap comes FIRST, ahead of every salvage gate: a clean tree with
	// HEAD already in origin/main is done, not salvage (ward#518, docs/container-reap.md).
	residual := revCount(ctx, r, work, "origin/main..HEAD")
	fmt.Fprintf(os.Stderr, "ward container reap: residual commit count against origin/main = %d\n", residual)
	cleanTree := strings.TrimSpace(statusSnapshot) == ""
	if residual == 0 && cleanTree { //nolint:nestif // clean-tree fast path carries the direct-main proof branch
		if env.Launched && env.Workflow.landsOnMain() && env.Issue != 0 {
			prov, perr := r.readRunProvenance(work)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "ward container reap: provenance missing or unreadable on already-landed direct-main run: %v\n", perr)
				return r.salvage(ctx, work, env, reasonConflict, false, nil, statusSnapshot,
					reapDecision{Gate: "provenance missing or unreadable on already-landed direct-main run", ProvState: "missing or unreadable"})
			}
			if !r.runProvenanceLanded(ctx, work, prov, env.Issue) {
				fmt.Fprintf(os.Stderr, "ward container reap: already-landed direct-main run is missing closes #%d; salvaging instead of returning success\n", env.Issue)
				return r.salvage(ctx, work, env, reasonCloseRef, false, nil, statusSnapshot,
					reapDecision{Gate: "missing same-repo closing reference on already-landed direct-main run", ProvState: "present", Landed: false})
			}
		}
		fmt.Fprintf(os.Stderr, "WARD-REAP: nothing to reap (%s)\n", reapBoundaryReason(env.Workflow))
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}

	prov, perr := r.readRunProvenance(work)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: provenance missing or unreadable: %v\n", perr)
		return r.salvage(ctx, work, env, reasonConflict, false, nil, statusSnapshot,
			reapDecision{Gate: "provenance missing or unreadable", ProvState: "missing or unreadable"})
	}
	// The run-owned-landed verdict is computed once here and reused by every gate
	// below (including executeReap) so the diagnostics block reports what each saw.
	landed := r.runProvenanceLanded(ctx, work, prov, env.Issue)
	if env.Issue != 0 && !landed && !r.issueClosingReferencePresent(ctx, work, env.Issue) {
		fmt.Fprintf(os.Stderr, "ward container reap: missing closes #%d in committed work; salvaging instead of landing on main\n", env.Issue)
		return r.salvage(ctx, work, env, reasonCloseRef, false, nil, statusSnapshot,
			reapDecision{Gate: "missing same-repo closing reference", ProvState: "present", Landed: landed})
	}

	findings := scan.Diff(r.diffEntries(ctx, work, "origin/main...HEAD"))
	if !landed {
		fmt.Fprintln(os.Stderr, "ward container reap: no run-owned landed commit after dispatch; salvaging instead of claiming success")
		return r.salvage(ctx, work, env, reasonConflict, false, findings, statusSnapshot,
			reapDecision{Gate: "no run-owned landed commit after dispatch", ProvState: "present"})
	}

	action := decideReap(reapInputs{
		HasResidualWork:  residual > 0 || strings.TrimSpace(statusSnapshot) != "",
		IntegrationClean: r.integrate(ctx, work, residual),
		Findings:         findings,
	})
	fmt.Fprintf(os.Stderr, "ward container reap: decision=%d for %s\n", action, work)
	return r.executeReap(ctx, work, env, action, findings, statusSnapshot, landed, prov)
}

// gitEmptyTree is git's canonical empty-tree object (SHA-1); diffing against it
// renders every path in a tree as an addition (the no-base junk scan, ward#599).
const gitEmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// reapEstablishMain lands the empty-repo case (origin/main absent): create main
// from a clean, run-owned commit rather than salvage. See docs/container-reap.md.
func (r *Runner) reapEstablishMain(ctx context.Context, work string, env reapEnv, statusSnapshot string, releaseReservation bool) error {
	// No origin/main to diff against, so the whole HEAD history is residual work.
	residual := revCount(ctx, r, work, "HEAD")
	fmt.Fprintf(os.Stderr, "ward container cleanup: no origin/main; establish-main candidate with %d commit(s) on HEAD\n", residual)
	if residual == 0 {
		// The agent produced no landable commit (an unborn HEAD), so there is
		// nothing to establish main from and nothing to salvage.
		fmt.Fprintln(os.Stderr, "WARD-REAP: nothing to reap (empty repo with no commits to establish main from)")
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}

	// A pull-requests, pull-requests-and-merge, or patch-only run never lands on main.
	// That includes the establish-main case (ward#508).
	if !env.Workflow.landsOnMain() {
		findings := scan.Diff(r.diffEntries(ctx, work, gitEmptyTree+"..HEAD"))
		if len(findings) > 0 {
			fmt.Fprintf(os.Stderr, "ward container cleanup: --workflow %s produced a diff the junk scan rejected; preserving on a salvage branch instead of treating the workflow boundary as success\n", env.Workflow.orDefault())
			return r.salvage(ctx, work, env, reasonScan, false, findings, statusSnapshot,
				reapDecision{Gate: "junk scan flagged the workflow-boundary diff", ProvState: "not read (no origin/main)"})
		}
		fmt.Fprintf(os.Stderr, "WARD-REAP: nothing to reap (%s)\n", reapBoundaryReason(env.Workflow))
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}

	// Run-owned proof: the closing ref must sit in the committed history (an empty
	// repo has no stale history, so the origin/main provenance proof does not apply).
	if !r.issueClosingReferenceInRange(ctx, work, env.Issue, "HEAD") {
		fmt.Fprintf(os.Stderr, "ward container reap: missing closes #%d in committed work; salvaging instead of establishing main\n", env.Issue)
		return r.salvage(ctx, work, env, reasonCloseRef, false, nil, statusSnapshot,
			reapDecision{Gate: "missing same-repo closing reference", ProvState: "not read (no origin/main)"})
	}

	// Junk-scan the whole tree that would land: with no base ref, diff against
	// git's empty tree so every path shows as an addition.
	findings := scan.Diff(r.diffEntries(ctx, work, gitEmptyTree+"..HEAD"))
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "ward container reap: junk scan flagged %d path(s); salvaging instead of establishing main\n", len(findings))
		return r.salvage(ctx, work, env, reasonScan, false, findings, statusSnapshot,
			reapDecision{Gate: "junk scan flagged the diff", ProvState: "not read (no origin/main)"})
	}

	// Establish main: push HEAD as the new default branch.
	fmt.Fprintln(os.Stderr, "ward container reap: establishing main from run work (empty repo) start")
	out, perr := r.pushCapture(ctx, work, "HEAD:main")
	if perr == nil {
		fmt.Fprintln(os.Stderr, "ward container reap: established main from run work (empty repo)")
		return nil
	}
	// A rejected establish push is a real failure (branch protection, a main that
	// appeared mid-run, a dead/rotated PAT): classify and salvage.
	reason, authCause := reasonPushFail, false
	if isAuthFailure(out) {
		reason, authCause = reasonAuthFail, true
	}
	fmt.Fprintf(os.Stderr, "ward container reap: establish-main push rejected (%s); salvaging\n", reason)
	return r.salvage(ctx, work, env, reason, authCause, findings, statusSnapshot,
		reapDecision{Gate: "establish-main push rejected", ProvState: "not read (no origin/main)"})
}

// resolveReapWork picks the clone work tree: --work, then $WARD_REAP_WORK (set
// by the entrypoint), then the invoke cwd.
func resolveReapWork(c *cli.Command) string {
	if w := c.String("work"); w != "" {
		return w
	}
	if w := os.Getenv("WARD_REAP_WORK"); w != "" {
		return w
	}
	return resolveInvokeCWD()
}

// captureAndCommitResidual snapshots the target tree, then stages and commits
// whatever the agent left loose (bypassing hooks: preserve work, not re-gate it).
func (r *Runner) captureAndCommitResidual(ctx context.Context, work string, env reapEnv) string {
	return r.captureAndCommitResidualRepo(ctx, work, env.Mode, env.repo().slug())
}

// captureAndCommitResidualRepo is the per-repo half: snapshot, stage, and commit
// loose work in any clone (the target or a --repo grant), tagged with mode + slug.
func (r *Runner) captureAndCommitResidualRepo(ctx context.Context, work, mode, slug string) string {
	statusBytes, _ := r.Runner.Capture(ctx, "git", "-C", work, "status", "--porcelain")
	status := filterReapResidualStatus(string(statusBytes))
	fmt.Fprintf(os.Stderr, "ward container reap: capture residual status for %s (%s)\n", work, slug)
	_ = r.Runner.Exec(ctx, "git", "-C", work, "add", "-A", "--", ".", ":(exclude)"+runProvenanceFile)
	if hasStagedChanges(ctx, r, work) {
		// Tag the subject with the mode and carry the agent attribution as a
		// Co-Authored-By trailer (ward#155), naming who produced the work.
		msg := fmt.Sprintf("ward-container: residual %s work on %s\n\n%s",
			mode, slug, containerMode(mode).commitTrailer())
		if cerr := r.Runner.Exec(ctx, "git", "-C", work, "commit", "--no-verify", "-m", msg); cerr != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: residual commit failed: %v\n", cerr)
		} else {
			fmt.Fprintf(os.Stderr, "ward container reap: residual commit created for %s (%s)\n", work, slug)
		}
	}
	return status
}

// filterReapResidualStatus strips the reaper's own provenance artifact from the
// residual snapshot so a landed run can still hit the clean-tree fast path.
func filterReapResidualStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return ""
	}
	lines := strings.Split(status, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, runProvenanceFile) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// integrate rebases the residual work onto the latest main, reporting whether
// it applied cleanly; a conflict is aborted and reported as not-clean (salvage).
func (r *Runner) integrate(ctx context.Context, work string, residual int) bool {
	if residual == 0 {
		return true
	}
	fmt.Fprintf(os.Stderr, "ward container reap: rebase start for %s onto origin/main\n", work)
	if rerr := r.Runner.Exec(ctx, "git", "-C", work, "rebase", "origin/main"); rerr != nil {
		_ = r.Runner.Exec(ctx, "git", "-C", work, "rebase", "--abort")
		fmt.Fprintf(os.Stderr, "ward container reap: rebase failed for %s\n", work)
		return false
	}
	fmt.Fprintf(os.Stderr, "ward container reap: rebase clean for %s\n", work)
	return true
}

// executeReap carries out the decided action: do nothing, push to main (falling
// to salvage if the push is rejected), or salvage.
func (r *Runner) executeReap(ctx context.Context, work string, env reapEnv, action reapAction, findings []scan.Finding, status string, landed bool, prov runProvenance) error {
	switch action {
	case reapNothing:
		fmt.Fprintln(os.Stderr, "ward container reap: nothing to reap")
		return nil
	case reapPushMain:
		return r.executePushMainReap(ctx, work, env, findings, status, landed, prov)
	case reapSalvage:
		reason, gate := reasonConflict, "merge conflict integrating onto main"
		if len(findings) > 0 {
			reason, gate = reasonScan, "junk scan flagged the diff"
		}
		return r.salvage(ctx, work, env, reason, false, findings, status,
			reapDecision{Gate: gate, ProvState: "present", Landed: true})
	}
	return nil
}

func (r *Runner) executePushMainReap(ctx context.Context, work string, env reapEnv, findings []scan.Finding, status string, landed bool, prov runProvenance) error {
	if !landed {
		fmt.Fprintln(os.Stderr, "ward container reap: remote main has no run-owned commit after dispatch; salvaging")
		return r.salvage(ctx, work, env, reasonConflict, false, findings, status,
			reapDecision{Gate: "remote main has no run-owned commit (pre-push recheck)", ProvState: "present"})
	}
	if err := r.ensureClosingReferenceBeforePush(ctx, work, env, findings, status, landed, prov); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "ward container reap: push to main start")
	out, perr := r.pushCapture(ctx, work, "HEAD:main")
	if perr == nil {
		fmt.Fprintln(os.Stderr, "ward container reap: landed on main")
		return nil
	}
	// Classify the rejection so the salvage issue distinguishes a dead/rotated
	// PAT (auth) from the remote simply having advanced (race) - see ward#103.
	reason, authCause := reasonPushRace, false
	if isAuthFailure(out) {
		reason, authCause = reasonAuthFail, true
	}
	fmt.Fprintf(os.Stderr, "ward container reap: push to main rejected (%s); salvaging\n", reason)
	return r.salvage(ctx, work, env, reason, authCause, findings, status,
		reapDecision{Gate: "push to main rejected", ProvState: "present", Landed: true})
}

func (r *Runner) ensureClosingReferenceBeforePush(ctx context.Context, work string, env reapEnv, findings []scan.Finding, status string, landed bool, prov runProvenance) error {
	if env.Issue == 0 || r.issueClosingReferencePresent(ctx, work, env.Issue) {
		return nil
	}
	// Final closing-ref gate LOCAL to the irreversible push (ward#515): re-check
	// the post-rebase history so no upstream-gate reorder can land a close-refless run.
	if !closingReferenceRepairSafe(prov, env) {
		fmt.Fprintf(os.Stderr, "ward container reap: closing reference for #%d absent from the history about to land; salvaging instead of pushing main\n", env.Issue)
		return r.salvage(ctx, work, env, reasonCloseRef, false, findings, status,
			reapDecision{Gate: "missing same-repo closing reference (push-site recheck)", ProvState: "present", Landed: landed})
	}
	fmt.Fprintf(os.Stderr, "ward container reap: closing reference for #%d absent from the history about to land; repairing before push\n", env.Issue)
	if err := r.repairClosingReference(ctx, work, env); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: closing reference repair failed: %v\n", err)
		return r.salvage(ctx, work, env, reasonCloseRef, false, findings, status,
			reapDecision{Gate: "missing same-repo closing reference (push-site repair failed)", ProvState: "present", Landed: landed})
	}
	if !r.issueClosingReferencePresent(ctx, work, env.Issue) {
		return r.salvage(ctx, work, env, reasonCloseRef, false, findings, status,
			reapDecision{Gate: "missing same-repo closing reference (push-site repair failed)", ProvState: "present", Landed: landed})
	}
	fmt.Fprintf(os.Stderr, "ward container reap: repaired closing reference for #%d\n", env.Issue)
	return nil
}

func (r *Runner) readRunProvenance(work string) (runProvenance, error) {
	var prov runProvenance
	data, err := os.ReadFile(filepath.Join(work, runProvenanceFile))
	if err != nil {
		return prov, fmt.Errorf("read run provenance: %w", err)
	}
	if uerr := json.Unmarshal(data, &prov); uerr != nil {
		return prov, fmt.Errorf("parse run provenance: %w", uerr)
	}
	return prov, nil
}

func (r *Runner) runProvenanceLanded(ctx context.Context, work string, prov runProvenance, issue int) bool {
	if issue == 0 || prov.BaselineMain == "" {
		return false
	}
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "log", "--format=%H%x00%cI%x00%B%x00", prov.BaselineMain+"..origin/main")
	if err != nil {
		return false
	}
	reservedAt, rerr := time.Parse(time.RFC3339, prov.ReservedAt)
	if rerr != nil {
		return false
	}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		hash := strings.TrimSpace(fields[i])
		tsRaw := strings.TrimSpace(fields[i+1])
		body := fields[i+2]
		if hash == "" || tsRaw == "" {
			continue
		}
		ts, terr := time.Parse(time.RFC3339, tsRaw)
		if terr != nil || ts.Before(reservedAt) {
			continue
		}
		if issueClosingReferenceTextPresent(body, issue) {
			return true
		}
	}
	return false
}

func closingReferenceRepairSafe(prov runProvenance, env reapEnv) bool {
	return env.Issue != 0 && prov.Issue == env.Issue && prov.Repo == env.repo().slug()
}

func (r *Runner) repairClosingReference(ctx context.Context, work string, env reapEnv) error {
	if env.Issue == 0 {
		return fmt.Errorf("no carried issue")
	}
	if r.issueClosingReferencePresent(ctx, work, env.Issue) {
		return nil
	}
	if revCount(ctx, r, work, "origin/main..HEAD") == 1 {
		subj, _ := r.Runner.Capture(ctx, "git", "-C", work, "log", "-1", "--format=%s")
		if strings.HasPrefix(strings.TrimSpace(string(subj)), "ward-container: residual ") {
			msg, err := r.Runner.Capture(ctx, "git", "-C", work, "log", "-1", "--format=%B")
			if err != nil {
				return err
			}
			return r.amendClosingReference(ctx, work, string(msg), env.Issue)
		}
	}
	subject := fmt.Sprintf("ward-container: repair closing reference for %s#%d", env.repo().slug(), env.Issue)
	return r.Runner.Exec(ctx, "git", "-C", work, "commit", "--allow-empty", "-m", subject, "-m", fmt.Sprintf("closes #%d", env.Issue))
}

func (r *Runner) amendClosingReference(ctx context.Context, work, msg string, issue int) error {
	repaired := appendClosingReferenceToMessage(msg, issue)
	tmp, err := os.CreateTemp("", "ward-closing-reference-*.txt")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(repaired); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return r.Runner.Exec(ctx, "git", "-C", work, "commit", "--amend", "-F", tmp.Name())
}

func appendClosingReferenceToMessage(msg string, issue int) string {
	if issueClosingReferenceTextPresent(msg, issue) {
		return strings.TrimRight(msg, "\n") + "\n"
	}
	return strings.TrimRight(msg, "\n") + fmt.Sprintf("\n\ncloses #%d\n", issue)
}

// salvage preserves residual work on a ward-salvage/<id> branch (durable) then
// best-effort files/appends a forgejo issue (notification); the branch goes first.
func (r *Runner) salvage(ctx context.Context, work string, env reapEnv, reason reapReason, authCause bool, findings []scan.Finding, status string, dec reapDecision) error {
	id := env.Name + "-" + randHex()
	branch := salvageBranchName(id)
	fmt.Fprintf(os.Stderr, "ward container reap: salvage start container=%s branch=%s reason=%s\n", env.Container, branch, reason)

	// Dump the debugging block to stderr FIRST (ward#531): a dead-PAT salvage files
	// no issue, so the container log is the only surface these facts reach.
	age, _ := formatTokenAge(env.UpAt, time.Now())
	diag := r.gatherReapDiagnostics(ctx, work, reason, dec, status, age)
	fmt.Fprintf(os.Stderr, "%s\n", renderReapDiagnostics(diag))

	_ = r.Runner.Exec(ctx, "git", "-C", work, "branch", "-f", branch, "HEAD")
	if out, perr := r.pushCapture(ctx, work, branch+":"+branch); perr != nil {
		// The branch push reuses the same baked PAT, so a dead token fails here too;
		// classify it so the log names the cause - no issue can be filed either (ward#103).
		if isAuthFailure(out) {
			authCause = true
		}
		cause := ""
		if authCause {
			cause = " on auth (the baked Forgejo PAT was likely rotated/revoked mid-run; no salvage issue could be filed for the same reason)"
		}
		// Remote unreachable: the container log is the only durable surface left,
		// so emit the patch for recovery via `docker logs` before teardown.
		fmt.Fprintf(os.Stderr, "ward container reap: salvage branch push failed%s (%v); dumping patch to log as last resort\n", cause, perr)
		r.dumpPatch(ctx, work)
		return fmt.Errorf("ward container reap: could not preserve work to the remote: %w", perr)
	}
	fmt.Fprintf(os.Stderr, "ward container reap: preserved work on %s (%s)\n", branch, reason)

	report := salvageReport{
		Repo:        env.repo(),
		Mode:        env.Mode,
		Branch:      branch,
		Reason:      reason,
		AuthCause:   authCause,
		TokenAge:    age,
		Findings:    findings,
		Status:      status,
		Base:        env.Base,
		Issue:       env.Issue,
		Diagnostics: diag,
	}
	if ferr := r.fileSalvageIssue(ctx, env, report); ferr != nil {
		// The branch already preserved the work; a failed issue is a missed
		// notification, not lost work. Log loudly and succeed.
		fmt.Fprintf(os.Stderr, "ward container reap: filed branch but could not file issue: %v\n", ferr)
	}
	return nil
}

// fileSalvageIssue posts the salvage notice: a carried run comments on its own
// issue, a freeform run files a standalone one (ward#518, docs/container-reap.md).
func (r *Runner) fileSalvageIssue(ctx context.Context, env reapEnv, report salvageReport) error {
	if env.Token == "" {
		return fmt.Errorf("no FORGEJO_TOKEN to file a salvage issue")
	}
	// The ops mount authenticates from $FORGEJO_TOKEN in-container (forgejoTokenResolver),
	// so the reaper drives the same client host flows do.
	var fc salvageNotifier
	switch env.Forge {
	case forgeGitLab:
		cl, err := r.hostGitLabClient(ctx, containerMode(env.Mode))
		if err != nil {
			return err
		}
		cl.token = env.Token
		fc = cl
	default:
		cl, err := r.hostForgejoClient(ctx)
		if err != nil {
			return err
		}
		fc = cl.withMode(containerMode(env.Mode)).withToken(env.Token)
	}
	return notifySalvage(ctx, fc, env, report)
}

// salvageNotifier is the Forgejo surface notifySalvage drives; *forgejoClient
// satisfies it in production and a fake stands in for tests (ward#518).
type salvageNotifier interface {
	reopenIssue(ctx context.Context, owner, repo string, number int) error
	commentIssue(ctx context.Context, owner, repo string, number int, body string) error
	createIssue(ctx context.Context, owner, repo, title, body string) (int, error)
	repoPullRequestsEnabled(ctx context.Context, owner, repo string) (bool, error)
	createPullRequest(ctx context.Context, owner, repo, head, base, title, body string) (string, error)
}

// notifySalvage routes the salvage notice (ward#518): a carried run reopens +
// comments on its issue, a freeform run files one standalone issue (never append).
func notifySalvage(ctx context.Context, fc salvageNotifier, env reapEnv, report salvageReport) error {
	report = openSalvagePullRequest(ctx, fc, env, report)
	if env.Issue != 0 {
		// Reopen first (best-effort, idempotent) so the issue never reads "done"
		// over unmerged work, then post the notice.
		if rerr := fc.reopenIssue(ctx, env.Owner, env.Name, env.Issue); rerr != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: could not reopen carried issue #%d: %v\n", env.Issue, rerr)
		}
		if cerr := fc.commentIssue(ctx, env.Owner, env.Name, env.Issue, salvageCommentBody(report)); cerr != nil {
			return cerr
		}
		fmt.Fprintf(os.Stderr, "ward container reap: posted salvage notice to carried issue #%d\n", env.Issue)
		return nil
	}
	n, err := fc.createIssue(ctx, env.Owner, env.Name, salvageIssueTitle(report), salvageIssueBody(report))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ward container reap: filed standalone salvage issue #%d\n", n)
	return nil
}

func openSalvagePullRequest(ctx context.Context, fc salvageNotifier, env reapEnv, report salvageReport) salvageReport {
	if strings.TrimSpace(report.Branch) == "" {
		report.PullRequestUnavailable = "salvage branch was not created"
		return report
	}
	enabled, err := fc.repoPullRequestsEnabled(ctx, env.Owner, env.Name)
	if err != nil {
		report.PullRequestUnavailable = "could not check repo PR support: " + firstLine(err.Error())
		return report
	}
	if !enabled {
		report.PullRequestUnavailable = "pull requests are disabled for this repo"
		return report
	}
	title := fmt.Sprintf("ward salvage: %s", report.Branch)
	body := salvagePullRequestBody(report)
	if report.Issue != 0 {
		body = strings.TrimRight(body, "\n") + fmt.Sprintf("\n\ncloses #%d\n", report.Issue)
	}
	url, err := fc.createPullRequest(ctx, env.Owner, env.Name, report.Branch, "main", title, body)
	if err != nil {
		report.PullRequestUnavailable = "PR creation failed: " + firstLine(err.Error())
		return report
	}
	report.PullRequestURL = url
	return report
}

func salvagePullRequestBody(report salvageReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ward preserved residual work from `%s` on `%s`.\n\n", report.Repo.slug(), report.Branch)
	fmt.Fprintf(&b, "- **Reason:** %s\n", report.Reason)
	if report.Issue != 0 {
		fmt.Fprintf(&b, "- **Carried issue:** #%d\n", report.Issue)
	}
	b.WriteString("\nReview the branch before merging. The issue thread carries the full reaper diagnostics.\n")
	return b.String()
}

// releaseReservationIfUnstarted retracts the remote issue reservation on a clean
// reap when the agent never launched (ward#264, docs/agent-reservation.md).
func (r *Runner) releaseReservationIfUnstarted(ctx context.Context, env reapEnv) {
	if !env.reservationReleasable() {
		fmt.Fprintf(os.Stderr, "ward container reap: reservation keep for #%d (launched=%t issue=%d)\n", env.Issue, env.Launched, env.Issue)
		return
	}
	if env.Token == "" {
		fmt.Fprintln(os.Stderr, "ward container reap: no FORGEJO_TOKEN to release the issue reservation")
		return
	}
	var fc Tracker
	switch env.Forge {
	case forgeGitLab:
		cl, err := r.hostGitLabClient(ctx, containerMode(env.Mode))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: could not build gitlab client to release reservation: %v\n", err)
			return
		}
		cl.token = env.Token
		fc = cl
	default:
		cl, err := r.hostForgejoClient(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: could not build forgejo client to release reservation: %v\n", err)
			return
		}
		fc = cl.withMode(containerMode(env.Mode)).withToken(env.Token)
	}
	// Name the specific pre-launch gate that died (auth / ollama-probe / bootstrap),
	// its error line, and the recovery step - not just "smoke-test death" (ward#609).
	gate, _ := readGateFailure()
	body := reservationReleaseCommentBody(containerMode(env.Mode), env.Name, gate)
	if err := fc.commentIssue(ctx, env.Owner, env.Name, env.Issue, body); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not release issue reservation on #%d: %v\n", env.Issue, err)
		return
	}
	// Retract the reservation's conversation lock (ward#494) so a retry lands on an
	// open thread; best-effort, silent on the no-lock-leaf forge (Forgejo today).
	if err := fc.unlockIssue(ctx, env.Owner, env.Name, env.Issue); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward container reap: could not unlock issue #%d after release: %v\n", env.Issue, err)
	}
	fmt.Fprintf(os.Stderr, "ward container reap: released issue reservation on #%d (container exited pre-launch, did no work)\n", env.Issue)
}

// --- granted-repo (--repo) push verification (ward#291) ----------------------

// verifyExtraReposLanded checks each --repo grant landed on its remote main before
// the run reads as done (ward#291); an un-pushed grant is preserved + surfaced.
func (r *Runner) verifyExtraReposLanded(ctx context.Context, env reapEnv) {
	if env.ReadOnly || len(env.ExtraRepos) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "ward container reap: verifying %d granted repo(s)\n", len(env.ExtraRepos))
	var unlanded []extraRepoUnlanded
	for _, repo := range env.ExtraRepos {
		work := extraRepoWorkDir(repo)
		if !isGitWorkTree(ctx, r, work) {
			// The bootstrap clone never landed (already logged there): nothing to
			// verify and nothing to recover, so don't flag a phantom failure.
			fmt.Fprintf(os.Stderr, "ward container reap: granted repo %s has no clone at %s; skipping push verification\n", repo.slug(), work)
			continue
		}
		if rep, landed := r.checkExtraRepoLanded(ctx, env, repo, work); !landed {
			unlanded = append(unlanded, rep)
		}
	}
	if len(unlanded) == 0 {
		fmt.Fprintln(os.Stderr, "ward container reap: all granted repos verified landed on main")
		return
	}
	r.reportUnlandedExtraRepos(ctx, env, unlanded)
}

// grantLandingFetchAttempts / grantLandingFetchBackoff bound the propagation window
// the reaper waits for a granted-repo push to show on origin/main (ward#583, docs).
const (
	grantLandingFetchAttempts = 3
	grantLandingFetchBackoff  = 2 * time.Second
)

// grantLandingSleep is the backoff wait between propagation-window re-fetches, a
// package var so a test stubs the real sleep out.
var grantLandingSleep = time.Sleep

// checkExtraRepoLanded reports whether a grant's local work is reachable from its
// freshly-fetched origin/main; un-landed work is committed + preserved first.
func (r *Runner) checkExtraRepoLanded(ctx context.Context, env reapEnv, repo targetRepo, work string) (extraRepoUnlanded, bool) {
	status := r.captureAndCommitResidualRepo(ctx, work, env.Mode, repo.slug())
	rep := extraRepoUnlanded{Repo: repo, Status: status}

	// A grant landed iff its local HEAD is REACHABLE from origin/main (ancestry), not
	// iff HEAD equals it: a merge-commit/lagged push lands HEAD as a proper ancestor.
	landed, hasMain := r.grantLanded(ctx, work)
	if !hasMain {
		// No remote main to compare against: we cannot prove the work landed, so
		// treat it as un-landed and preserve whatever HEAD holds.
		rep.NoMain = true
		r.preserveExtraRepo(ctx, work, env, &rep)
		return rep, false
	}
	if landed {
		// HEAD is contained in origin/main: landed. The closing-ref discipline is the
		// TARGET repo's gate, not the grant's (its empty range would false-flag it, #583).
		return extraRepoUnlanded{}, true
	}

	// Genuinely un-landed: count the truly-missing commits (git cherry's `+` lines),
	// not the raw ahead count that different-hash-but-landed commits inflate (ward#587).
	missing := r.unlandedPatchCount(ctx, work)
	if missing <= 0 {
		missing = revCount(ctx, r, work, "origin/main..HEAD")
		if missing == 0 {
			missing = 1 // grantLanded already ruled this un-landed: at least one is missing.
		}
	}
	rep.Ahead = missing
	r.preserveExtraRepo(ctx, work, env, &rep)
	return rep, false
}

// grantLanded fetches origin and reports whether the grant's work is present on
// origin/main - by reachability (ward#583) or by patch-id (ward#587); see docs.
func (r *Runner) grantLanded(ctx context.Context, work string) (landed, hasMain bool) {
	for attempt := 1; attempt <= grantLandingFetchAttempts; attempt++ {
		_ = r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin")
		if refExists(ctx, r, work, "origin/main") {
			hasMain = true
			// Reachability: HEAD contained in origin/main (plain or merge-commit landing).
			if isAncestor(ctx, r, work, "HEAD", "origin/main") {
				return true, true
			}
			// Content: HEAD diverged, but zero un-landed patches means the run's changes
			// are all on main under a different hash - a landing, not a loss (ward#587).
			if r.unlandedPatchCount(ctx, work) == 0 {
				return true, true
			}
		}
		if attempt < grantLandingFetchAttempts {
			grantLandingSleep(grantLandingFetchBackoff)
		}
	}
	return false, hasMain
}

// unlandedPatchCount counts local commits ahead of origin/main with NO patch-equivalent
// upstream (git cherry's `+` lines); zero means content-landed. -1 on error (ward#587).
func (r *Runner) unlandedPatchCount(ctx context.Context, work string) int {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "cherry", "origin/main", "HEAD")
	if err != nil {
		return -1
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			count++
		}
	}
	return count
}

// issueClosingReferencePresent reports whether the committed range mentions the
// carried issue closing trailer the same repo needs before landing.
func (r *Runner) issueClosingReferencePresent(ctx context.Context, work string, issue int) bool {
	return r.issueClosingReferenceInRange(ctx, work, issue, "origin/main..HEAD")
}

// issueClosingReferenceInRange is the range-parameterized form: normal path checks
// origin/main..HEAD, empty-repo establish-main checks whole-HEAD history (ward#599).
func (r *Runner) issueClosingReferenceInRange(ctx context.Context, work string, issue int, rangeRef string) bool {
	if issue == 0 {
		return true
	}
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "log", "--format=%B", rangeRef)
	if err != nil {
		return false
	}

	// Enforce machine-checkable closure references on commits created after reservation.
	// That rejects a wrong trailer like "closes #425" while carrying issue #426.
	commits := strings.Split(strings.TrimSpace(string(out)), "\n\n")

	// Handle the empty-log edge case.
	if len(commits) == 0 || (len(commits) == 1 && strings.TrimSpace(commits[0]) == "") {
		return false
	}

	for _, commit := range commits {
		trimmedCommit := strings.TrimSpace(commit)
		if trimmedCommit != "" && issueClosingReferenceTextPresent(trimmedCommit, issue) {
			return true
		}
	}

	return false
}

func issueClosingReferenceTextPresent(text string, issue int) bool {
	if issue == 0 {
		return true
	}
	return issueClosingReferenceRE(issue).MatchString(text)
}

func issueClosingReferenceRE(issue int) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b(?:closes|fixes|resolves)\s+#` + regexp.QuoteMeta(strconv.Itoa(issue)) + `\b`)
}

// preserveExtraRepo pushes a granted repo's un-landed work to a salvage branch so
// it survives teardown; a push failure falls back to dumping the patch to the log.
func (r *Runner) preserveExtraRepo(ctx context.Context, work string, _ reapEnv, rep *extraRepoUnlanded) {
	branch := salvageBranchName(rep.Repo.Name + "-" + randHex())
	_ = r.Runner.Exec(ctx, "git", "-C", work, "branch", "-f", branch, "HEAD")
	if out, perr := r.pushCapture(ctx, work, branch+":"+branch); perr != nil {
		if rep.PushErr = strings.TrimSpace(out); rep.PushErr == "" {
			rep.PushErr = perr.Error()
		}
		fmt.Fprintf(os.Stderr, "ward container reap: granted repo %s salvage-branch push failed (%v); dumping patch to log\n", rep.Repo.slug(), perr)
		r.dumpPatch(ctx, work)
		return
	}
	rep.Branch = branch
	fmt.Fprintf(os.Stderr, "ward container reap: preserved un-landed granted-repo work on %s (%s)\n", branch, rep.Repo.slug())
}

// reportUnlandedExtraRepos undoes the run's apparent success: it reopens the target
// issue (cancelling any `closes #N`) and comments which grants did not land.
func (r *Runner) reportUnlandedExtraRepos(ctx context.Context, env reapEnv, reports []extraRepoUnlanded) {
	for _, rep := range reports {
		fmt.Fprintf(os.Stderr, "ward container reap: granted repo %s did NOT land (%d un-pushed commit(s))\n", rep.Repo.slug(), rep.Ahead)
	}
	if env.Issue == 0 {
		fmt.Fprintln(os.Stderr, "ward container reap: no target issue to flag the un-landed granted repos on")
		return
	}
	if env.Token == "" {
		fmt.Fprintln(os.Stderr, "ward container reap: no FORGEJO_TOKEN to flag the un-landed granted repos on the issue")
		return
	}
	var fc Tracker
	switch env.Forge {
	case forgeGitLab:
		cl, err := r.hostGitLabClient(ctx, containerMode(env.Mode))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: could not build gitlab client to flag un-landed granted repos: %v\n", err)
			return
		}
		cl.token = env.Token
		fc = cl
	default:
		cl, err := r.hostForgejoClient(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: could not build forgejo client to flag un-landed granted repos: %v\n", err)
			return
		}
		fc = cl.withMode(containerMode(env.Mode)).withToken(env.Token)
	}
	// Reopen first (idempotent on an already-open issue), then comment: the issue
	// must not read "done" while a granted repo's committed work is unreachable.
	if rerr := fc.reopenIssue(ctx, env.Owner, env.Name, env.Issue); rerr != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not reopen issue #%d: %v\n", env.Issue, rerr)
	}
	if cerr := fc.commentIssue(ctx, env.Owner, env.Name, env.Issue, unlandedExtraReposComment(env, reports)); cerr != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not comment un-landed granted repos on #%d: %v\n", env.Issue, cerr)
		return
	}
	fmt.Fprintf(os.Stderr, "ward container reap: reopened #%d and flagged %d un-landed granted repo(s)\n", env.Issue, len(reports))
}

// dumpPatch writes the residual diff to stderr as a final recovery surface when
// the remote is unreachable; the container log outlives the container.
func (r *Runner) dumpPatch(ctx context.Context, work string) {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "format-patch", "origin/main..HEAD", "--stdout")
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		out, _ = r.Runner.Capture(ctx, "git", "-C", work, "diff", "HEAD")
	}
	fmt.Fprintf(os.Stderr, "----- ward container reap: UNPRESERVED PATCH (recover from this log) -----\n%s\n----- end patch -----\n", string(out))
}

// diffEntries parses `git diff --numstat` into scan-ready entries, pairing each
// path with its worktree size and binary flag (--no-renames splits renames).
func (r *Runner) diffEntries(ctx context.Context, work, rangeRef string) []scan.Entry {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "diff", "--no-renames", "--numstat", rangeRef)
	if err != nil {
		return nil
	}
	var entries []scan.Entry
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := fields[2]
		e := scan.Entry{Path: path, Binary: fields[0] == "-" && fields[1] == "-"}
		// #nosec G304,G703 -- read-only Size() stat of a path git itself just
		// reported in this repo's own diff; no file contents are opened.
		if fi, statErr := os.Stat(filepath.Join(work, path)); statErr == nil {
			e.Bytes = fi.Size()
		}
		entries = append(entries, e)
	}
	return entries
}

// pushCapture runs `git push origin <refspec>`, teeing git's stderr diagnostics
// to the live log while capturing them so a failure can be classified (ward#103).
func (r *Runner) pushCapture(ctx context.Context, work, refspec string) (string, error) {
	var buf bytes.Buffer
	prev := r.Runner.Stderr
	if prev == nil {
		prev = io.Discard
	}
	r.Runner.Stderr = io.MultiWriter(prev, &buf)
	err := r.Runner.Exec(ctx, "git", "-C", work, "push", "origin", refspec)
	r.Runner.Stderr = prev
	return buf.String(), err
}

// --- reap diagnostics (ward#531) ---------------------------------------------

// gatherReapDiagnostics assembles the debugging block a salvage/fail site emits
// (ward#531): reaper ward version, HEAD-vs-origin/main, decision/provenance facts.
func (r *Runner) gatherReapDiagnostics(ctx context.Context, work string, reason reapReason, dec reapDecision, status, tokenAge string) reapDiagnostics {
	version, source := wardVersionResolution()
	main := shortSha(r.captureRev(ctx, work, "origin/main"))
	return reapDiagnostics{
		WardVersion:   version,
		VersionSource: source,
		Head:          shortSha(r.captureRev(ctx, work, "HEAD")),
		OriginMain:    main,
		HeadOnMain:    main != "" && isAncestor(ctx, r, work, "HEAD", "origin/main"),
		Gate:          dec.Gate,
		Reason:        reason,
		ProvState:     dec.ProvState,
		Landed:        dec.Landed,
		Status:        status,
		TokenAge:      tokenAge,
	}
}

// wardVersionResolution reports the reaper's compiled ward version and how it
// resolved (WARD_VERSION/--ward-version pin vs releases/latest) - the #504 key field.
func wardVersionResolution() (version, source string) {
	pin := strings.TrimSpace(os.Getenv("WARD_VERSION"))
	if pin == "" || pin == "dev" {
		return Version, "releases/latest (resolved in-container)"
	}
	return Version, fmt.Sprintf("pinned via WARD_VERSION/--ward-version (%s)", pin)
}

// captureRev resolves a ref to its full sha, or "" when git cannot (no such ref).
func (r *Runner) captureRev(ctx context.Context, work, ref string) string {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "rev-parse", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// shortSha truncates a full sha to a readable 12 chars for the block.
func shortSha(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// isAncestor reports `git merge-base --is-ancestor a b` (a is contained in b).
func isAncestor(ctx context.Context, r *Runner, work, a, b string) bool {
	return r.Runner.Exec(ctx, "git", "-C", work, "merge-base", "--is-ancestor", a, b) == nil
}

// --- small git predicates ----------------------------------------------------

func isGitWorkTree(ctx context.Context, r *Runner, work string) bool {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func hasStagedChanges(ctx context.Context, r *Runner, work string) bool {
	// `git diff --cached --quiet` exits non-zero when there are staged changes.
	return r.Runner.Exec(ctx, "git", "-C", work, "diff", "--cached", "--quiet") != nil
}

func refExists(ctx context.Context, r *Runner, work, ref string) bool {
	return r.Runner.Exec(ctx, "git", "-C", work, "rev-parse", "--verify", "--quiet", ref) == nil
}

func revCount(ctx context.Context, r *Runner, work, rangeRef string) int {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "rev-list", "--count", rangeRef)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}
