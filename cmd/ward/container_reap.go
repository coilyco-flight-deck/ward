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
// verb the entrypoint runs on every agent exit. See docs/container-lifecycle.md.

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
	// surface (ward#517). See docs/container-lifecycle.md.
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
	// Empty reads as merge-remote-main; PR and remote-branch-only modes stay on a branch.
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
		return "workflow pull-request boundary reached with a clean local tree. The reaper did not verify remote branch, pull request, or CI state"
	case workflowPullRequestAndMerge:
		return "workflow pull-request-and-merge boundary reached with a clean local tree. The reaper did not verify remote branch, pull request, or CI state"
	case workflowRemoteBranchOnly:
		return "workflow remote-branch-only boundary reached with a clean local tree. The reaper did not verify remote branch state"
	default:
		return "tree clean, workflow boundary reached"
	}
}

func containerReapCommand() *cli.Command {
	return &cli.Command{
		Name:   "reap",
		Hidden: true, // ward#263: entrypoint-internal, not a hand-run verb
		Usage:  "Clean up residual work before container teardown: land it on main if clean, else push a salvage branch and file an issue.",
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
	unlanded := r.verifyExtraReposLanded(ctx, env)
	if terr == nil && !unlanded {
		r.commentLaunchedNoOutcomeIfNeeded(ctx, env)
	}
	r.releaseReservationIfTerminalOutcome(ctx, env)
	return terr
}

// reapTargetTree is the target half of a reap, from capture through salvage.
func (r *Runner) reapTargetTree(ctx context.Context, work string, env reapEnv, releaseReservation bool) error { //nolint:gocognit,gocyclo,cyclop,nestif,funlen
	if env.ReadOnly {
		fmt.Fprintln(os.Stderr, "ward container reap: read-only session, nothing to salvage (skipping)")
		return nil
	}
	commitState := residualCommitState(ctx, r, work)
	statusSnapshot := r.captureAndCommitResidual(ctx, work, env)
	fmt.Fprintf(os.Stderr, "ward container reap: residual status snapshot for %s: %q\n", work, strings.TrimSpace(statusSnapshot))

	// Refresh remote-tracking refs before deciding whether work landed.
	fmt.Fprintln(os.Stderr, "ward container reap: fetch origin start")
	if err := r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin"); err != nil {
		return fmt.Errorf("ward container reap: cannot refresh the workflow destination, landing state is unknown: %w", err)
	}
	fmt.Fprintln(os.Stderr, "ward container reap: fetch origin done")
	if !refExists(ctx, r, work) {
		// Empty repo (no base branch): establish main from a clean run rather than
		// salvage it just for starting empty (ward#599, docs/container-lifecycle.md).
		return r.reapEstablishMain(ctx, work, env, statusSnapshot, releaseReservation, commitState)
	}

	if !env.Workflow.landsOnMain() {
		findings := scan.Diff(r.diffEntries(ctx, work, "origin/main...HEAD"))
		if len(findings) > 0 {
			fmt.Fprintf(os.Stderr, "ward container cleanup: --workflow %s produced a diff the junk scan rejected; preserving on a salvage branch instead of treating the workflow boundary as success\n", env.Workflow.orDefault())
			return r.salvage(ctx, work, env, reasonScan, false, findings, statusSnapshot,
				reapDecision{Gate: "junk scan flagged the workflow-boundary diff", ProvState: "not read (workflow hold)", CommitState: commitState})
		}
		fmt.Fprintf(os.Stderr, "WARD-REAP: nothing to reap (%s)\n", reapBoundaryReason(env.Workflow))
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}

	codeLanded, residualDiff, landingProof, err := r.targetGitLandingEvidence(ctx, work)
	if err != nil {
		return fmt.Errorf("ward container reap: cannot determine whether code landed: %w", err)
	}
	if codeLanded {
		r.reportLandedTarget(ctx, work, env, landingProof, false)
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}
	if !residualDiff {
		return fmt.Errorf("ward container reap: contradictory Git evidence: HEAD is not contained in origin/main, but no recoverable residual diff exists")
	}

	residual := revCount(ctx, r, work, "origin/main..HEAD")
	fmt.Fprintf(os.Stderr, "ward container reap: residual commit count against origin/main = %d\n", residual)

	prov, perr := r.readRunProvenance(work)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: provenance missing or unreadable: %v\n", perr)
		return r.salvage(ctx, work, env, reasonConflict, false, nil, statusSnapshot,
			reapDecision{Gate: "provenance missing or unreadable", ProvState: "missing or unreadable", CommitState: commitState})
	}
	// The run-owned-landed verdict is computed once here and reused by every gate
	// below (including executeReap) so the diagnostics block reports what each saw.
	landed := r.runProvenanceLanded(ctx, work, prov, env.Issue)
	if env.Issue != 0 && !landed && !r.issueClosingReferencePresent(ctx, work, env.Issue) {
		repaired, err := r.repairResidualClosingReference(ctx, work, env, commitState)
		if err != nil {
			return r.salvage(ctx, work, env, reasonCloseRef, false, nil, statusSnapshot,
				reapDecision{Gate: err.Error(), ProvState: "present", CommitState: commitState, Landed: false})
		}
		landed = repaired
	}

	findings := scan.Diff(r.diffEntries(ctx, work, "origin/main...HEAD"))
	if !landed {
		fmt.Fprintln(os.Stderr, "ward container reap: no run-owned landed commit after dispatch; salvaging instead of claiming success")
		return r.salvage(ctx, work, env, reasonConflict, false, findings, statusSnapshot,
			reapDecision{Gate: "no run-owned landed commit after dispatch", ProvState: "present", CommitState: commitState})
	}

	action := decideReap(reapInputs{
		HasResidualWork:  residual > 0 || strings.TrimSpace(statusSnapshot) != "",
		IntegrationClean: r.integrate(ctx, work, residual),
		Findings:         findings,
	})
	fmt.Fprintf(os.Stderr, "ward container reap: decision=%d for %s\n", action, work)
	return r.executeReap(ctx, work, env, action, findings, statusSnapshot, landed, prov, commitState)
}

// gitEmptyTree is git's canonical empty-tree object (SHA-1); diffing against it
// renders every path in a tree as an addition (the no-base junk scan, ward#599).
const gitEmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// reapEstablishMain lands the empty-repo case (origin/main absent): create main
// from a clean, run-owned commit rather than salvage. See docs/container-lifecycle.md.
func (r *Runner) reapEstablishMain(ctx context.Context, work string, env reapEnv, statusSnapshot string, releaseReservation bool, commitState string) error {
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

	// A pull-request, pull-request-and-merge, or remote-branch-only run never lands on main.
	// That includes the establish-main case (ward#508).
	if !env.Workflow.landsOnMain() {
		findings := scan.Diff(r.diffEntries(ctx, work, gitEmptyTree+"..HEAD"))
		if len(findings) > 0 {
			fmt.Fprintf(os.Stderr, "ward container cleanup: --workflow %s produced a diff the junk scan rejected; preserving on a salvage branch instead of treating the workflow boundary as success\n", env.Workflow.orDefault())
			return r.salvage(ctx, work, env, reasonScan, false, findings, statusSnapshot,
				reapDecision{Gate: "junk scan flagged the workflow-boundary diff", ProvState: "not read (no origin/main)", CommitState: commitState})
		}
		fmt.Fprintf(os.Stderr, "WARD-REAP: nothing to reap (%s)\n", reapBoundaryReason(env.Workflow))
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}

	// Run-owned proof: the closing ref must sit in the committed history (an empty
	// repo has no stale history, so the origin/main provenance proof does not apply).
	canEstablish, err := r.ensureEmptyRepoClosingReference(ctx, work, env, statusSnapshot, commitState)
	if err != nil {
		return err
	}
	if !canEstablish {
		// A successful salvage is a terminal outcome, not permission to fall
		// through and establish main from the same close-less work.
		return nil
	}

	// Junk-scan the whole tree that would land: with no base ref, diff against
	// git's empty tree so every path shows as an addition.
	findings := scan.Diff(r.diffEntries(ctx, work, gitEmptyTree+"..HEAD"))
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "ward container reap: junk scan flagged %d path(s); salvaging instead of establishing main\n", len(findings))
		return r.salvage(ctx, work, env, reasonScan, false, findings, statusSnapshot,
			reapDecision{Gate: "junk scan flagged the diff", ProvState: "not read (no origin/main)", CommitState: commitState})
	}
	if err := r.runPreCommitGate(ctx, work); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: %v; salvaging\n", err)
		return r.salvage(ctx, work, env, reasonPreCommit, false, findings, statusSnapshot,
			reapDecision{Gate: "pre-commit suite rejected the residual tree", ProvState: "not read (no origin/main)", CommitState: commitState})
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
		reapDecision{Gate: "establish-main push rejected", ProvState: "not read (no origin/main)", CommitState: commitState})
}

func (r *Runner) repairResidualClosingReference(ctx context.Context, work string, env reapEnv, commitState string) (bool, error) {
	if commitState == commitStateAgentDidNotCommit {
		repaired, err := r.repairDirtyOnlyClosingReference(ctx, work, env)
		if err != nil {
			return false, fmt.Errorf("dirty-only closing reference repair failed")
		}
		return repaired, nil
	}
	// Only the reaper's own residual capture may receive the carried trailer.
	// Changing a pre-teardown commit would weaken the same-repo proof.
	fmt.Fprintf(os.Stderr, "ward container reap: committed work is missing %s; salvaging without landing\n", closingReferenceLabel(env))
	return false, fmt.Errorf("commit existed but lacked close trailer")
}

// ensureEmptyRepoClosingReference reports whether establish-main may continue.
// A false, nil result means the work was successfully preserved on salvage.
func (r *Runner) ensureEmptyRepoClosingReference(ctx context.Context, work string, env reapEnv, statusSnapshot, commitState string) (bool, error) {
	if r.issueClosingReferenceInRange(ctx, work, env.Issue, "HEAD") {
		return true, nil
	}
	if commitState == commitStateAgentDidNotCommit {
		fmt.Fprintf(os.Stderr, "ward container reap: agent did not commit before teardown; repairing the reaper residual commit with %s\n", closingReferenceLabel(env))
		if err := r.repairClosingReference(ctx, work, env); err == nil && r.issueClosingReferenceInRange(ctx, work, env.Issue, "HEAD") {
			fmt.Fprintf(os.Stderr, "ward container reap: repaired closing reference for %s\n", closingReferenceTarget(env))
			return true, nil
		}
		fmt.Fprintln(os.Stderr, "ward container reap: reaper residual closing reference repair failed; salvaging instead of establishing main")
		return false, r.salvage(ctx, work, env, reasonCloseRef, false, nil, statusSnapshot,
			reapDecision{Gate: "agent did not commit and residual closing reference repair failed", ProvState: "not read (no origin/main)", CommitState: commitState})
	}
	fmt.Fprintf(os.Stderr, "ward container reap: committed work is missing %s; salvaging instead of establishing main\n", closingReferenceLabel(env))
	return false, r.salvage(ctx, work, env, reasonCloseRef, false, nil, statusSnapshot,
		reapDecision{Gate: "commit existed but lacked close trailer", ProvState: "not read (no origin/main)", CommitState: commitState})
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
	if strings.TrimSpace(status) != "" {
		if err := validateGitCommitIdentity(ctx, r.Runner, work); err != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: residual commit refused before staging: %v\n", err)
			return status
		}
	}
	_ = r.Runner.Exec(ctx, "git", "-C", work, "add", "-A", "--", ".", ":(exclude)"+runProvenanceFile)
	if hasStagedChanges(ctx, r, work) {
		// Tag the subject with the mode and carry the agent attribution as a
		// Co-Authored-By trailer (ward#155), naming who produced the work.
		msg := fmt.Sprintf("ward-container: residual %s work on %s\n\n%s",
			mode, slug, containerMode(mode).commitTrailer())
		cerr := validateGitCommitIdentity(ctx, r.Runner, work)
		if cerr == nil {
			cerr = r.Runner.Exec(ctx, "git", gitUseConfigOnlyArgv(work, "commit", "--no-verify", "-m", msg)...)
		}
		if cerr != nil {
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
func (r *Runner) executeReap(ctx context.Context, work string, env reapEnv, action reapAction, findings []scan.Finding, status string, landed bool, prov runProvenance, commitState string) error {
	switch action {
	case reapNothing:
		fmt.Fprintln(os.Stderr, "ward container reap: nothing to reap")
		return nil
	case reapPushMain:
		return r.executePushMainReap(ctx, work, env, findings, status, landed, prov, commitState)
	case reapSalvage:
		reason, gate := reasonConflict, "merge conflict integrating onto main"
		if len(findings) > 0 {
			reason, gate = reasonScan, "junk scan flagged the diff"
		}
		return r.salvage(ctx, work, env, reason, false, findings, status,
			reapDecision{Gate: gate, ProvState: "present", CommitState: commitState, Landed: true})
	}
	return nil
}

func (r *Runner) executePushMainReap(ctx context.Context, work string, env reapEnv, findings []scan.Finding, status string, landed bool, prov runProvenance, commitState string) error {
	if !landed {
		fmt.Fprintln(os.Stderr, "ward container reap: remote main has no run-owned commit after dispatch; salvaging")
		return r.salvage(ctx, work, env, reasonConflict, false, findings, status,
			reapDecision{Gate: "remote main has no run-owned commit (pre-push recheck)", ProvState: "present", CommitState: commitState})
	}
	if err := r.ensureClosingReferenceBeforePush(ctx, work, env, findings, status, landed, prov, commitState); err != nil {
		return err
	}
	if err := r.runPreCommitGate(ctx, work); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: %v; salvaging\n", err)
		return r.salvage(ctx, work, env, reasonPreCommit, false, findings, status,
			reapDecision{Gate: "pre-commit suite rejected the residual tree", ProvState: "present", CommitState: commitState, Landed: landed})
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
		reapDecision{Gate: "push to main rejected", ProvState: "present", CommitState: commitState, Landed: true})
}

func (r *Runner) runPreCommitGate(ctx context.Context, work string) error {
	if !isFile(filepath.Join(work, ".pre-commit-config.yaml")) {
		fmt.Fprintln(os.Stderr, "ward container reap: no .pre-commit-config.yaml; skipping pre-commit gate")
		return nil
	}
	if !commandExists("pre-commit") {
		return fmt.Errorf("pre-commit is not on PATH")
	}
	fmt.Fprintf(os.Stderr, "ward container reap: pre-commit run --all-files start for %s\n", work)
	if err := r.execIn(ctx, work, "pre-commit", "run", "--all-files"); err != nil {
		return fmt.Errorf("pre-commit run --all-files failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "ward container reap: pre-commit gate clean for %s\n", work)
	return nil
}

func (r *Runner) ensureClosingReferenceBeforePush(ctx context.Context, work string, env reapEnv, findings []scan.Finding, status string, landed bool, prov runProvenance, commitState string) error {
	if env.Issue == 0 || r.issueClosingReferencePresent(ctx, work, env.Issue) {
		return nil
	}
	// Final closing-ref gate LOCAL to the irreversible push (ward#515): re-check
	// the post-rebase history so no upstream-gate reorder can land a close-refless run.
	if !closingReferenceRepairSafe(prov, env) {
		fmt.Fprintf(os.Stderr, "ward container reap: closing reference for %s absent from the history about to land; salvaging instead of pushing main\n", closingReferenceTarget(env))
		return r.salvage(ctx, work, env, reasonCloseRef, false, findings, status,
			reapDecision{Gate: "missing same-repo closing reference (push-site recheck)", ProvState: "present", CommitState: commitState, Landed: landed})
	}
	fmt.Fprintf(os.Stderr, "ward container reap: closing reference for %s absent from the history about to land; repairing before push\n", closingReferenceTarget(env))
	if err := r.repairClosingReference(ctx, work, env); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: closing reference repair failed: %v\n", err)
		return r.salvage(ctx, work, env, reasonCloseRef, false, findings, status,
			reapDecision{Gate: "missing same-repo closing reference (push-site repair failed)", ProvState: "present", CommitState: commitState, Landed: landed})
	}
	if !r.issueClosingReferencePresent(ctx, work, env.Issue) {
		return r.salvage(ctx, work, env, reasonCloseRef, false, findings, status,
			reapDecision{Gate: "missing same-repo closing reference (push-site repair failed)", ProvState: "present", CommitState: commitState, Landed: landed})
	}
	fmt.Fprintf(os.Stderr, "ward container reap: repaired closing reference for %s\n", closingReferenceTarget(env))
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

func (r *Runner) targetGitLandingEvidence(ctx context.Context, work string) (landed, residual bool, proof string, err error) {
	status, err := r.Runner.Capture(ctx, "git", "-C", work, "status", "--porcelain")
	if err != nil {
		return false, false, "", fmt.Errorf("read working tree status: %w", err)
	}
	if strings.TrimSpace(filterReapResidualStatus(string(status))) != "" {
		return false, false, "", fmt.Errorf("working tree remains dirty after residual capture")
	}
	head := r.captureRev(ctx, work, "HEAD")
	main := r.captureRev(ctx, work, "origin/main")
	if head == "" || main == "" {
		return false, false, "", fmt.Errorf("resolve HEAD and origin/main")
	}
	if head == main {
		return true, false, "head-equals-origin-main", nil
	}
	if headOnOriginMain(ctx, r, work) {
		return true, false, "head-contained-in-origin-main", nil
	}
	cherry, err := r.Runner.Capture(ctx, "git", "-C", work, "cherry", "origin/main", "HEAD")
	if err != nil {
		return false, false, "", fmt.Errorf("compare patch landing against origin/main: %w", err)
	}
	missing := false
	for _, line := range strings.Split(strings.TrimSpace(string(cherry)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			missing = true
			break
		}
	}
	if !missing {
		return true, false, "patch-equivalent-content-on-origin-main", nil
	}
	diff, err := r.Runner.Capture(ctx, "git", "-C", work, "diff", "--name-only", "origin/main...HEAD")
	if err != nil {
		return false, false, "", fmt.Errorf("inspect residual diff against origin/main: %w", err)
	}
	return false, strings.TrimSpace(string(diff)) != "", "unlanded-residual-diff", nil
}

func (r *Runner) reportLandedTarget(ctx context.Context, work string, env reapEnv, proof string, recoveryArtifact bool) {
	tracker := "not-applicable"
	if env.Issue != 0 {
		tracker = "reconciliation-required"
		if r.issueClosingReferenceInRange(ctx, work, env.Issue, "origin/main") {
			tracker = "agrees"
		}
	}
	fmt.Fprintf(os.Stderr, "WARD-REAP: code-landed=true tracker-bookkeeping=%s recovery-artifact=%t proof=%s\n", tracker, recoveryArtifact, proof)
	if tracker == "reconciliation-required" {
		fmt.Fprintf(os.Stderr, "ward container reap: code is already on origin/main, but %s is absent; report tracker reconciliation without salvage or reopen\n", closingReferenceLabel(env))
	}
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
			return r.amendClosingReference(ctx, work, string(msg), env)
		}
	}
	subject := fmt.Sprintf("ward-container: repair closing reference for %s", closingReferenceTarget(env))
	if err := validateGitCommitIdentity(ctx, r.Runner, work); err != nil {
		return err
	}
	return r.Runner.Exec(ctx, "git", gitUseConfigOnlyArgv(work, "commit", "--allow-empty", "-m", subject, "-m", closingReferenceLabel(env))...)
}

func (r *Runner) repairDirtyOnlyClosingReference(ctx context.Context, work string, env reapEnv) (bool, error) {
	if env.Issue == 0 || r.issueClosingReferencePresent(ctx, work, env.Issue) {
		return true, nil
	}
	fmt.Fprintf(os.Stderr, "ward container reap: dirty-only run is missing %s; repairing before landing\n", closingReferenceLabel(env))
	if err := r.repairClosingReference(ctx, work, env); err != nil {
		return false, err
	}
	if !r.issueClosingReferencePresent(ctx, work, env.Issue) {
		return false, fmt.Errorf("dirty-only closing reference repair failed")
	}
	return true, nil
}

func (r *Runner) amendClosingReference(ctx context.Context, work, msg string, env reapEnv) error {
	if err := validateGitCommitIdentity(ctx, r.Runner, work); err != nil {
		return err
	}
	repaired := appendClosingReferenceToMessage(msg, env)
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
	return r.Runner.Exec(ctx, "git", gitUseConfigOnlyArgv(work, "commit", "--amend", "-F", tmp.Name())...)
}

func appendClosingReferenceToMessage(msg string, env reapEnv) string {
	if issueClosingReferenceTextPresent(msg, env.Issue) {
		return strings.TrimRight(msg, "\n") + "\n"
	}
	return strings.TrimRight(msg, "\n") + "\n\n" + closingReferenceLabel(env) + "\n"
}

// salvage preserves residual work on a ward-salvage/<id> branch (durable) then
// best-effort files/appends a forgejo issue (notification); the branch goes first.
func (r *Runner) salvage(ctx context.Context, work string, env reapEnv, reason reapReason, authCause bool, findings []scan.Finding, status string, dec reapDecision) error {
	required, err := r.salvageRequired(ctx, work, env)
	if err != nil || !required {
		return err
	}

	id := env.Name + "-" + randHex()
	branch := salvageBranchName(id)
	fmt.Fprintf(os.Stderr, "ward container reap: salvage start container=%s branch=%s reason=%s\n", env.Container, branch, reason)

	// Dump the debugging block to stderr FIRST (ward#531): a dead-PAT salvage files
	// no issue, so the container log is the only surface these facts reach.
	age, _ := formatTokenAge(env.UpAt, time.Now())
	diag := r.gatherReapDiagnostics(ctx, work, reason, dec, status, age)
	fmt.Fprintf(os.Stderr, "%s\n", renderReapDiagnostics(diag))

	if out, perr := r.pushCapture(ctx, work, "HEAD:refs/heads/"+branch); perr != nil {
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
	if err := r.verifyRemoteSalvageBranch(ctx, work, branch); err != nil {
		fmt.Fprintf(os.Stderr, "WARD-REAP: code-landed=unknown tracker-bookkeeping=not-mutated recovery-artifact=unknown proof=salvage-branch-verification-failed\n")
		r.dumpPatch(ctx, work)
		return fmt.Errorf("ward container reap: salvage push returned success but the recovery artifact could not be verified: %w", err)
	}
	continueSalvage, err := r.reconcileSalvageAfterPush(ctx, work, env, branch)
	if err != nil || !continueSalvage {
		return err
	}
	fmt.Fprintf(os.Stderr, "ward container reap: preserved work on %s (%s)\n", branch, reason)
	fmt.Fprintf(os.Stderr, "WARD-REAP: code-landed=false tracker-bookkeeping=%s recovery-artifact=true proof=verified-remote-branch\n", salvageTrackerBookkeeping(env))

	report := salvageReport{
		Repo:                     env.repo(),
		Mode:                     env.Mode,
		Branch:                   branch,
		Reason:                   reason,
		CommitState:              diag.CommitState,
		AuthCause:                authCause,
		TokenAge:                 age,
		Findings:                 findings,
		Status:                   status,
		Base:                     env.Base,
		Issue:                    env.Issue,
		Diagnostics:              diag,
		RecoveryArtifactVerified: true,
	}
	if ferr := r.fileSalvageIssue(ctx, env, report); ferr != nil {
		// The branch already preserved the work; a failed issue is a missed
		// notification, not lost work. Log loudly and succeed.
		fmt.Fprintf(os.Stderr, "ward container reap: filed branch but could not file issue: %v\n", ferr)
	}
	return nil
}

func (r *Runner) salvageRequired(ctx context.Context, work string, env reapEnv) (bool, error) {
	codeLanded, residualDiff, landingProof, err := r.refreshTargetGitLandingEvidence(ctx, work)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARD-REAP: code-landed=unknown tracker-bookkeeping=not-mutated recovery-artifact=false proof=destination-refresh-failed\n")
		r.dumpPatch(ctx, work)
		return false, fmt.Errorf("ward container reap: cannot salvage while destination landing state is unknown: %w", err)
	}
	if codeLanded {
		r.reportLandedTarget(ctx, work, env, landingProof, false)
		return false, nil
	}
	if !residualDiff {
		return false, fmt.Errorf("ward container reap: refusing salvage because Git found no recoverable residual diff")
	}
	return true, nil
}

func (r *Runner) reconcileSalvageAfterPush(ctx context.Context, work string, env reapEnv, branch string) (bool, error) {
	codeLanded, residualDiff, landingProof, err := r.refreshTargetGitLandingEvidence(ctx, work)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARD-REAP: code-landed=unknown tracker-bookkeeping=not-mutated recovery-artifact=true proof=post-push-destination-refresh-failed\n")
		return false, fmt.Errorf("ward container reap: recovery artifact exists at %s, but destination landing state is now unknown: %w", branch, err)
	}
	if codeLanded {
		if derr := r.removeRemoteSalvageBranch(ctx, work, branch); derr != nil {
			r.reportLandedTarget(ctx, work, env, landingProof, true)
			return false, fmt.Errorf("ward container reap: code landed while salvage was being preserved, but redundant recovery branch %s could not be removed: %w", branch, derr)
		}
		r.reportLandedTarget(ctx, work, env, landingProof, false)
		return false, nil
	}
	if !residualDiff {
		if derr := r.removeRemoteSalvageBranch(ctx, work, branch); derr != nil {
			fmt.Fprintf(os.Stderr, "WARD-REAP: code-landed=unknown tracker-bookkeeping=not-mutated recovery-artifact=true proof=no-residual-diff\n")
			return false, fmt.Errorf("ward container reap: no residual diff remains, but redundant recovery branch %s could not be removed: %w", branch, derr)
		}
		return false, fmt.Errorf("ward container reap: refusing salvage because refreshed Git evidence found no recoverable residual diff")
	}
	return true, nil
}

func (r *Runner) refreshTargetGitLandingEvidence(ctx context.Context, work string) (landed, residual bool, proof string, err error) {
	if err := r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin"); err != nil {
		return false, false, "", fmt.Errorf("refresh origin: %w", err)
	}
	if !refExists(ctx, r, work) {
		return false, true, "origin-main-absent", nil
	}
	return r.targetGitLandingEvidence(ctx, work)
}

func (r *Runner) verifyRemoteSalvageBranch(ctx context.Context, work, branch string) error {
	head := r.captureRev(ctx, work, "HEAD")
	if head == "" {
		return fmt.Errorf("resolve local HEAD")
	}
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != head || fields[1] != "refs/heads/"+branch {
		return fmt.Errorf("remote branch does not resolve to local HEAD")
	}
	return nil
}

func (r *Runner) removeRemoteSalvageBranch(ctx context.Context, work, branch string) error {
	if _, err := r.pushCapture(ctx, work, ":refs/heads/"+branch); err != nil {
		return err
	}
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("remote branch still exists after deletion")
	}
	return nil
}

func salvageTrackerBookkeeping(env reapEnv) string {
	if env.Issue == 0 {
		return "not-applicable"
	}
	return "reopen-required"
}

func (r *Runner) salvagePullRequestWouldBeEmpty(ctx context.Context, work string) bool {
	_ = r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin")
	if !refExists(ctx, r, work) {
		return false
	}
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "diff", "--name-only", "origin/main...HEAD")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not inspect salvage branch diff before opening PR: %v\n", err)
		return false
	}
	return strings.TrimSpace(string(out)) == ""
}

// fileSalvageIssue posts the salvage notice: a carried run comments on its own
// issue, a freeform run files a standalone one (ward#518, docs/container-lifecycle.md).
func (r *Runner) fileSalvageIssue(ctx context.Context, env reapEnv, report salvageReport) error {
	if env.Token == "" {
		return fmt.Errorf("no FORGEJO_TOKEN to file a salvage issue")
	}
	// The ops mount authenticates from $FORGEJO_TOKEN in-container (forgejoTokenResolver),
	// so the reaper drives the same client host flows do.
	var fc salvageNotifier
	switch env.Forge {
	case forgeGitLab:
		cl := r.hostGitLabClient(ctx, containerMode(env.Mode))
		cl.token = env.Token
		fc = cl
	case forgeForgejo, forgeGitHub:
		cl := r.hostForgejoClient(ctx)
		fc = cl.withMode(containerMode(env.Mode)).withToken(env.Token)
	}
	return notifySalvage(ctx, fc, env, report)
}

// salvageNotifier is the Forgejo surface notifySalvage drives; *forgejoClient
// satisfies it in production and a fake stands in for tests (ward#518).
type salvageNotifier interface {
	ReopenIssue(ctx context.Context, owner, repo string, number int) error
	CommentIssue(ctx context.Context, owner, repo string, number int, body string) error
	CreateIssue(ctx context.Context, owner, repo, title, body string) (int, error)
	RepoPullRequestsEnabled(ctx context.Context, owner, repo string) (bool, error)
	CreatePullRequest(ctx context.Context, owner, repo, head, base, title, body string) (string, error)
}

// notifySalvage routes the salvage notice (ward#518): a carried run reopens +
// comments on its issue, a freeform run files one standalone issue (never append).
func notifySalvage(ctx context.Context, fc salvageNotifier, env reapEnv, report salvageReport) error {
	if !report.RecoveryArtifactVerified || strings.TrimSpace(report.Branch) == "" {
		return fmt.Errorf("refusing tracker mutation without a verified recovery artifact")
	}
	if report.PullRequestUnavailable == salvagePullRequestEmptyReason {
		return fmt.Errorf("refusing tracker mutation for an empty salvage diff")
	}
	report = openSalvagePullRequest(ctx, fc, env, report)
	if env.Issue != 0 {
		// Reopen first (best-effort, idempotent) so the issue never reads "done"
		// over unmerged work, then post the notice.
		if rerr := fc.ReopenIssue(ctx, env.Owner, env.Name, env.Issue); rerr != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: could not reopen carried issue %s: %v\n", reapIssueLabel(env), rerr)
		}
		if cerr := fc.CommentIssue(ctx, env.Owner, env.Name, env.Issue, salvageCommentBody(report)); cerr != nil {
			return cerr
		}
		fmt.Fprintf(os.Stderr, "ward container reap: posted salvage notice to carried issue %s\n", reapIssueLabel(env))
		return nil
	}
	n, err := fc.CreateIssue(ctx, env.Owner, env.Name, salvageIssueTitle(report), salvageIssueBody(report))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ward container reap: filed standalone salvage issue %s\n", repoIssueLabel(env.Owner, env.Name, n))
	return nil
}

func openSalvagePullRequest(ctx context.Context, fc salvageNotifier, env reapEnv, report salvageReport) salvageReport {
	if strings.TrimSpace(report.PullRequestURL) != "" || strings.TrimSpace(report.PullRequestUnavailable) != "" {
		return report
	}
	if strings.TrimSpace(report.Branch) == "" {
		report.PullRequestUnavailable = "salvage branch was not created"
		return report
	}
	enabled, err := fc.RepoPullRequestsEnabled(ctx, env.Owner, env.Name)
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
		body = strings.TrimRight(body, "\n") + fmt.Sprintf("\n\ncloses %s#%d\n", report.Repo.slug(), report.Issue)
	}
	url, err := fc.CreatePullRequest(ctx, env.Owner, env.Name, report.Branch, "main", title, body)
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
		fmt.Fprintf(&b, "- **Carried issue:** %s\n", salvageIssueRef(report))
	}
	b.WriteString("\nReview the branch before merging. The issue thread carries the full reaper diagnostics.\n")
	return b.String()
}

// releaseReservationIfUnstarted retracts the remote issue reservation on a clean
// reap when the agent never launched (ward#264, docs/agent-reservation.md).
func (r *Runner) releaseReservationIfUnstarted(ctx context.Context, env reapEnv) {
	if !env.reservationReleasable() {
		fmt.Fprintf(os.Stderr, "ward container reap: reservation keep for %s (launched=%t issue=%d)\n", reapIssueLabel(env), env.Launched, env.Issue)
		return
	}
	if env.Token == "" {
		fmt.Fprintln(os.Stderr, "ward container reap: no FORGEJO_TOKEN to release the issue reservation")
		return
	}
	var fc Tracker
	switch env.Forge {
	case forgeGitLab:
		cl := r.hostGitLabClient(ctx, containerMode(env.Mode))
		cl.token = env.Token
		fc = cl
	case forgeForgejo, forgeGitHub:
		cl := r.hostForgejoClient(ctx)
		fc = cl.withMode(containerMode(env.Mode)).withToken(env.Token)
	}
	// Name the specific pre-launch gate that died (auth / ollama-probe / bootstrap),
	// its error line, and the recovery step - not just "smoke-test death" (ward#609).
	gate, _ := readGateFailure()
	body := reservationReleaseCommentBody(containerMode(env.Mode), env.Name, gate)
	if err := fc.CommentIssue(ctx, env.Owner, env.Name, env.Issue, body); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not release issue reservation on %s: %v\n", reapIssueLabel(env), err)
		return
	}
	// Retract the reservation's conversation lock (ward#494) so a retry lands on an
	// open thread; best-effort, silent on the no-lock-leaf forge (Forgejo today).
	if err := fc.UnlockIssue(ctx, env.Owner, env.Name, env.Issue); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "ward container reap: could not unlock issue %s after release: %v\n", reapIssueLabel(env), err)
	}
	deleteTransientWorkflowComments(ctx, fc, agentIssueRef{Owner: env.Owner, Repo: env.Name, Number: env.Issue}, time.Now().UTC())
	fmt.Fprintf(os.Stderr, "ward container reap: released issue reservation on %s (container exited pre-launch, did no work)\n", reapIssueLabel(env))
}

// releaseReservationIfTerminalOutcome retracts the remote reservation on run end.
// The canonical WARD-WORKFLOW outcome supersedes the hold and needs no override.
func (r *Runner) releaseReservationIfTerminalOutcome(ctx context.Context, env reapEnv) {
	if !env.Launched || env.Issue == 0 {
		return
	}
	upAt, err := time.Parse(time.RFC3339, strings.TrimSpace(env.UpAt))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: cannot parse WARD_CONTAINER_UP for terminal-outcome release: %v\n", err)
		return
	}
	fc := r.hostForgejoClient(ctx)
	if err := releaseReservationIfTerminalOutcomeComment(ctx, fc.withMode(containerMode(env.Mode)), env, upAt); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: %v\n", err)
	}
}

func releaseReservationIfTerminalOutcomeComment(ctx context.Context, fc Tracker, env reapEnv, afterAt time.Time) error {
	comments, err := fc.ListIssueComments(ctx, env.Owner, env.Name, env.Issue)
	if err != nil {
		return fmt.Errorf("could not read issue comments for terminal-outcome release on %s: %w", reapIssueLabel(env), err)
	}
	outcome, ok := latestBacklogOutcomeCommentAfter(comments, afterAt)
	if !ok {
		return nil
	}
	o, ok := backlogOutcomeOfComment(outcome.Body)
	if !ok || !terminalReservationOutcome(o.Status) {
		return nil
	}
	if shouldReleaseClosedUnmergedSubmittedOutcome(ctx, fc, env, o) {
		return releaseClosedUnmergedSubmittedOutcome(ctx, fc, env, outcome.CreatedAt, o)
	}
	// A reservation newer than the outcome means a follow-up run already took the
	// issue over; posting a release now would retract that live hold (ward#1149).
	if reservationCommentAfter(comments, outcome.CreatedAt) {
		fmt.Fprintf(os.Stderr, "ward container reap: keeping the terminal release off %s - a newer reservation holds the issue (%s)\n", reapIssueLabel(env), wardIssueURL(1149))
		return nil
	}
	body := terminalReservationReleaseCommentBody(containerMode(env.Mode), env.Container, o)
	if err := fc.CommentIssue(ctx, env.Owner, env.Name, env.Issue, body); err != nil {
		return fmt.Errorf("could not release terminal reservation on %s: %w", reapIssueLabel(env), err)
	}
	if err := fc.UnlockIssue(ctx, env.Owner, env.Name, env.Issue); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		return fmt.Errorf("could not unlock issue %s after terminal outcome release: %w", reapIssueLabel(env), err)
	}
	deleteTransientWorkflowComments(ctx, fc, agentIssueRef{Owner: env.Owner, Repo: env.Name, Number: env.Issue}, outcome.CreatedAt)
	fmt.Fprintf(os.Stderr, "ward container reap: released issue reservation on %s after terminal outcome %s\n", reapIssueLabel(env), o.Status)
	return nil
}

type pullRequestStateReader interface {
	GetPullRequest(context.Context, string, string, int) (*forgejoPullRequest, error)
	PullRequestMerged(context.Context, string, string, int) (bool, error)
}

func shouldReleaseClosedUnmergedSubmittedOutcome(ctx context.Context, fc Tracker, env reapEnv, outcome backlogOutcome) bool {
	if !strings.EqualFold(strings.TrimSpace(outcome.Status), "submitted") || strings.TrimSpace(outcome.PRURL) == "" {
		return false
	}
	reader, ok := fc.(pullRequestStateReader)
	if !ok || outcome.PRNumber <= 0 {
		return false
	}
	pr, err := reader.GetPullRequest(ctx, env.Owner, env.Name, outcome.PRNumber)
	if err != nil || pr == nil || !strings.EqualFold(strings.TrimSpace(pr.State), "closed") {
		return false
	}
	merged, err := reader.PullRequestMerged(ctx, env.Owner, env.Name, outcome.PRNumber)
	return err == nil && !merged
}

func releaseClosedUnmergedSubmittedOutcome(ctx context.Context, fc Tracker, env reapEnv, outcomeAt time.Time, outcome backlogOutcome) error {
	body := launchedClosedUnmergedPRCommentBody(env, outcome)
	if err := fc.CommentIssue(ctx, env.Owner, env.Name, env.Issue, body); err != nil {
		return fmt.Errorf("could not comment closed-unmerged PR failure on %s: %w", reapIssueLabel(env), err)
	}
	if err := fc.UnlockIssue(ctx, env.Owner, env.Name, env.Issue); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		return fmt.Errorf("could not unlock issue %s after closed-unmerged PR failure: %w", reapIssueLabel(env), err)
	}
	deleteTransientWorkflowComments(ctx, fc, agentIssueRef{Owner: env.Owner, Repo: env.Name, Number: env.Issue}, outcomeAt)
	fmt.Fprintf(os.Stderr, "ward container reap: released issue reservation on %s after closed-unmerged PR %s\n", reapIssueLabel(env), outcome.PRURL)
	return nil
}

func launchedClosedUnmergedPRCommentBody(env reapEnv, outcome backlogOutcome) string {
	var b strings.Builder
	visible := workflowOutcomeVisible("failed")
	fmt.Fprintf(&b, "`ward container reap` found a submitted pull request for this launched run, but Forgejo later closed it without merging.\n\n")
	fmt.Fprintf(&b, "- **Container:** `%s`\n", env.Container)
	fmt.Fprintf(&b, "- **Workflow:** `%s`\n", env.Workflow.orDefault())
	if outcome.PRURL != "" {
		fmt.Fprintf(&b, "- **PR:** `%s`\n", outcome.PRURL)
	}
	fmt.Fprintf(&b, "- **Recovery:** reopen the PR or cut a fresh run, then redispatch from the current base.\n")
	return collapsedIssueComment(visible, "reap details", b.String())
}

// reservationCommentAfter reports whether any reservation-marker comment (not a
// release) was posted strictly after at - a follow-up run's fresh hold (ward#1149).
func reservationCommentAfter(comments []issueComment, at time.Time) bool {
	for i := range comments {
		c := &comments[i]
		if !trustedMachineComment(*c, recordKindReservation) {
			continue
		}
		if c.CreatedAt.After(at) {
			return true
		}
	}
	return false
}

// terminalReservationOutcome reports whether an outcome status ends the run's hold;
// submitted/merge-ready are the PR workflows' final outcomes, so they count (ward#1149).
func terminalReservationOutcome(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "blocked", "failed", "merge-ready", "submitted":
		return true
	default:
		return false
	}
}

func terminalReservationReleaseCommentBody(mode containerMode, container string, outcome backlogOutcome) string {
	visible := workflowReservationReleasedVisible()
	var b strings.Builder
	fmt.Fprintf(&b, "Run finished with `%s`.\n\n", workflowOutcomeVisibleResult(outcome))
	fmt.Fprintf(&b, "`ward container reap` released container `%s` (`--harness %s`): the terminal outcome supersedes the reservation, so a later redispatch no longer needs `--override-reservation`.\n", container, mode)
	if summary := strings.TrimSpace(outcome.Text); summary != "" {
		fmt.Fprintf(&b, "\n**Outcome summary:** %s\n", summary)
	}
	return agentReservationReleaseMarker + "\n" + collapsedIssueComment(visible, "release details", b.String())
}

// commentLaunchedNoOutcomeIfNeeded marks a launched run failed when it exits with
// no WARD-WORKFLOW comment after it started and nothing residual to salvage.
func (r *Runner) commentLaunchedNoOutcomeIfNeeded(ctx context.Context, env reapEnv) {
	if !env.Launched || env.Issue == 0 {
		return
	}
	upAt, err := time.Parse(time.RFC3339, strings.TrimSpace(env.UpAt))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: cannot parse WARD_CONTAINER_UP for no-outcome check: %v\n", err)
		return
	}
	fc := r.hostForgejoClient(ctx)
	if err := postLaunchedNoOutcomeComment(ctx, fc.withMode(containerMode(env.Mode)), env, upAt); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: %v\n", err)
	}
}

// postLaunchedNoOutcomeComment marks a launched run failed when it exits with no
// WARD-WORKFLOW comment after it started and nothing residual to salvage.
func postLaunchedNoOutcomeComment(ctx context.Context, fc Tracker, env reapEnv, afterAt time.Time) error {
	comments, err := fc.ListIssueComments(ctx, env.Owner, env.Name, env.Issue)
	if err != nil {
		return fmt.Errorf("could not read issue comments for no-outcome check on #%d: %w", env.Issue, err)
	}
	if _, ok := latestBacklogOutcomeCommentAfter(comments, afterAt); ok {
		return nil
	}
	body := launchedNoOutcomeCommentBody(env)
	if err := fc.CommentIssue(ctx, env.Owner, env.Name, env.Issue, body); err != nil {
		return fmt.Errorf("could not comment launched no-outcome failure on #%d: %w", env.Issue, err)
	}
	if err := fc.UnlockIssue(ctx, env.Owner, env.Name, env.Issue); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		return fmt.Errorf("could not unlock issue #%d after no-outcome failure: %w", env.Issue, err)
	}
	return nil
}

// latestBacklogOutcomeCommentAfter returns the most recent WARD-WORKFLOW comment at
// or after afterAt, if any.
func latestBacklogOutcomeCommentAfter(comments []issueComment, afterAt time.Time) (issueComment, bool) {
	if humanFeedbackOutcomeBlocked(comments, time.Time{}) {
		return issueComment{}, false
	}
	var latest issueComment
	ok := false
	for _, c := range comments {
		if c.CreatedAt.Before(afterAt) {
			continue
		}
		if !trustedMachineComment(c, recordKindOutcome) {
			continue
		}
		if _, found := backlogOutcomeOfComment(c.Body); !found {
			continue
		}
		if !ok || c.CreatedAt.After(latest.CreatedAt) {
			latest = c
			ok = true
		}
	}
	return latest, ok
}

// launchedNoOutcomeCommentBody renders the failure comment for a launched run that
// exited without any WARD-WORKFLOW and without residual work to reap.
func launchedNoOutcomeCommentBody(env reapEnv) string {
	var b strings.Builder
	visible := workflowOutcomeVisible("failed")
	fmt.Fprintf(&b, "`ward container reap` found no residual work to salvage, but this launched run exited without a `WARD-WORKFLOW` comment.\n\n")
	fmt.Fprintf(&b, "- **Container:** `%s`\n", env.Container)
	fmt.Fprintf(&b, "- **Workflow:** `%s`\n", env.Workflow.orDefault())
	fmt.Fprintf(&b, "- **Recovery:** inspect the container log, fix the engineer seed or launch mode, and redispatch.\n")
	return collapsedIssueComment(visible, "reap details", b.String())
}

// --- granted-repo (--repo) push verification (ward#291) ----------------------

// verifyExtraReposLanded checks each --repo grant landed on its remote main before
// the run reads as done (ward#291); an un-pushed grant is preserved + surfaced.
func (r *Runner) verifyExtraReposLanded(ctx context.Context, env reapEnv) bool {
	if env.ReadOnly || len(env.ExtraRepos) == 0 {
		return false
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
		return false
	}
	r.reportUnlandedExtraRepos(ctx, env, unlanded)
	return true
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
		if refExists(ctx, r, work) {
			hasMain = true
			// Reachability: HEAD contained in origin/main (plain or merge-commit landing).
			if headOnOriginMain(ctx, r, work) {
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
	n := regexp.QuoteMeta(strconv.Itoa(issue))
	return regexp.MustCompile(`(?i)\b(?:closes|fixes|resolves)\s+(?:#|[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+#)` + n + `\b`)
}

func closingReferenceTarget(env reapEnv) string {
	if env.Issue == 0 {
		return "(no carried issue)"
	}
	return fmt.Sprintf("%s#%d", env.repo().slug(), env.Issue)
}

func closingReferenceLabel(env reapEnv) string {
	return "closes " + closingReferenceTarget(env)
}

func reapIssueLabel(env reapEnv) string {
	return repoIssueLabel(env.Owner, env.Name, env.Issue)
}

func repoIssueLabel(owner, repo string, issue int) string {
	if issue == 0 {
		return "(no issue)"
	}
	return fmt.Sprintf("%s/%s#%d", owner, repo, issue)
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
		cl := r.hostGitLabClient(ctx, containerMode(env.Mode))
		cl.token = env.Token
		fc = cl
	case forgeForgejo, forgeGitHub:
		cl := r.hostForgejoClient(ctx)
		fc = cl.withMode(containerMode(env.Mode)).withToken(env.Token)
	}
	// Reopen first (idempotent on an already-open issue), then comment: the issue
	// must not read "done" while a granted repo's committed work is unreachable.
	if rerr := fc.ReopenIssue(ctx, env.Owner, env.Name, env.Issue); rerr != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not reopen issue %s: %v\n", reapIssueLabel(env), rerr)
	}
	if cerr := fc.CommentIssue(ctx, env.Owner, env.Name, env.Issue, unlandedExtraReposComment(env, reports)); cerr != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not comment un-landed granted repos on %s: %v\n", reapIssueLabel(env), cerr)
		return
	}
	fmt.Fprintf(os.Stderr, "ward container reap: reopened %s and flagged %d un-landed granted repo(s)\n", reapIssueLabel(env), len(reports))
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
		HeadOnMain:    main != "" && headOnOriginMain(ctx, r, work),
		Gate:          dec.Gate,
		Reason:        reason,
		ProvState:     dec.ProvState,
		CommitState:   dec.CommitState,
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

// residualCommitState distinguishes a dirty-only run from one that already had
// a commit before the reaper snapshotted the tree.
func residualCommitState(ctx context.Context, r *Runner, work string) string {
	statusBytes, _ := r.Runner.Capture(ctx, "git", "-C", work, "status", "--porcelain")
	if refExists(ctx, r, work) {
		if revCount(ctx, r, work, "origin/main..HEAD") > 0 {
			return commitStateCommitExistedButLackedCloseTrailer
		}
	} else if revCount(ctx, r, work, "HEAD") > 0 {
		return commitStateCommitExistedButLackedCloseTrailer
	}
	if strings.TrimSpace(filterReapResidualStatus(string(statusBytes))) != "" {
		return commitStateAgentDidNotCommit
	}
	return ""
}

// headOnOriginMain reports whether HEAD is contained in origin/main.
func headOnOriginMain(ctx context.Context, r *Runner, work string) bool {
	return r.Runner.Exec(ctx, "git", "-C", work, "merge-base", "--is-ancestor", "HEAD", "origin/main") == nil
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

func refExists(ctx context.Context, r *Runner, work string) bool {
	return r.Runner.Exec(ctx, "git", "-C", work, "rev-parse", "--verify", "--quiet", "origin/main") == nil
}

func revCount(ctx context.Context, r *Runner, work, rangeRef string) int {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "rev-list", "--count", rangeRef)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}
