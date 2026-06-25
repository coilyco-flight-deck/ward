package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
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
	// ExtraRepos mirrors WARD_EXTRA_REPOS (ward#230): the --repo grants this run
	// cloned writable. The reaper verifies each one landed before done (ward#291).
	ExtraRepos []targetRepo
}

// envAgentLaunched is the entrypoint flag exported just before the agent launches,
// read by the reaper to tell a smoke-test death from a real agent run (ward#264).
const envAgentLaunched = "WARD_AGENT_LAUNCHED"

func readReapEnv() (reapEnv, error) {
	e := reapEnv{
		Owner:    os.Getenv("WARD_TARGET_OWNER"),
		Name:     os.Getenv("WARD_TARGET_NAME"),
		Base:     os.Getenv("WARD_FORGEJO_BASE"),
		Mode:     os.Getenv("WARD_MODE"),
		Token:    os.Getenv("FORGEJO_TOKEN"),
		UpAt:     os.Getenv("WARD_CONTAINER_UP"),
		Launched: os.Getenv(envAgentLaunched) == "1",
		ReadOnly: os.Getenv("WARD_READONLY") == "1",
	}
	// A missing/garbage WARD_TARGET_ISSUE parses to 0: "no issue to release".
	e.Issue, _ = strconv.Atoi(os.Getenv("WARD_TARGET_ISSUE"))
	e.ExtraRepos = parseExtraReposEnv(os.Getenv("WARD_EXTRA_REPOS"), e.Owner, e.Name)
	if e.Owner == "" || e.Name == "" || e.Base == "" {
		return e, fmt.Errorf("ward container reap: missing WARD_TARGET_OWNER/NAME/WARD_FORGEJO_BASE (run inside a ward container)")
	}
	if e.Mode == "" {
		e.Mode = "claude"
	}
	return e, nil
}

func (e reapEnv) repo() targetRepo { return targetRepo{Owner: e.Owner, Name: e.Name} }

// reservationReleasable reports whether a clean reap should retract this run's
// hold: only a container that carried an issue and never launched the agent (ward#264).
func (e reapEnv) reservationReleasable() bool {
	return !e.Launched && e.Issue != 0
}

func containerReapCommand() *cli.Command {
	return &cli.Command{
		Name:   "reap",
		Hidden: true, // ward#263: entrypoint-internal, not a hand-run verb
		Usage:  "Salvage residual work before container teardown: land it on main if clean, else push a salvage branch and file a forgejo issue.",
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
	terr := r.reapTargetTree(ctx, work, env, true)
	r.verifyExtraReposLanded(ctx, env)
	return terr
}

// reapTargetTree is the target half of a reap: capture -> integrate -> decide ->
// land or salvage; a fully-clean reap optionally releases the reservation (ward#264).
func (r *Runner) reapTargetTree(ctx context.Context, work string, env reapEnv, releaseReservation bool) error {
	if env.ReadOnly {
		fmt.Fprintln(os.Stderr, "ward container reap: read-only session, nothing to salvage (skipping)")
		return nil
	}
	statusSnapshot := r.captureAndCommitResidual(ctx, work, env)

	// Refresh remote-tracking refs so we integrate against the latest main; a
	// fetch failure leaves the clone-time origin/main as a usable base.
	_ = r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin")
	if !refExists(ctx, r, work, "origin/main") {
		// Without a main to integrate against we cannot safely push; preserve
		// whatever HEAD holds on a salvage branch.
		return r.salvage(ctx, work, env, reasonPushFail, false, nil, statusSnapshot)
	}

	residual := revCount(ctx, r, work, "origin/main..HEAD")
	if residual == 0 && strings.TrimSpace(statusSnapshot) == "" {
		fmt.Fprintln(os.Stderr, "ward container reap: nothing to reap (tree clean, HEAD on origin/main)")
		if releaseReservation {
			r.releaseReservationIfUnstarted(ctx, env)
		}
		return nil
	}

	findings := scanDiff(r.diffEntries(ctx, work, "origin/main...HEAD"))
	action := decideReap(reapInputs{
		HasResidualWork:  residual > 0,
		IntegrationClean: r.integrate(ctx, work, residual),
		Findings:         findings,
	})
	return r.executeReap(ctx, work, env, action, findings, statusSnapshot)
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
	status, _ := r.Runner.Capture(ctx, "git", "-C", work, "status", "--porcelain")
	_ = r.Runner.Exec(ctx, "git", "-C", work, "add", "-A")
	if hasStagedChanges(ctx, r, work) {
		// Tag the subject with the mode and carry the agent attribution as a
		// Co-Authored-By trailer (ward#155), naming who produced the work.
		msg := fmt.Sprintf("ward-container: residual %s work on %s\n\n%s",
			mode, slug, containerMode(mode).commitTrailer())
		if cerr := r.Runner.Exec(ctx, "git", "-C", work, "commit", "--no-verify", "-m", msg); cerr != nil {
			fmt.Fprintf(os.Stderr, "ward container reap: residual commit failed: %v\n", cerr)
		}
	}
	return string(status)
}

// integrate rebases the residual work onto the latest main, reporting whether
// it applied cleanly; a conflict is aborted and reported as not-clean (salvage).
func (r *Runner) integrate(ctx context.Context, work string, residual int) bool {
	if residual == 0 {
		return true
	}
	if rerr := r.Runner.Exec(ctx, "git", "-C", work, "rebase", "origin/main"); rerr != nil {
		_ = r.Runner.Exec(ctx, "git", "-C", work, "rebase", "--abort")
		return false
	}
	return true
}

// executeReap carries out the decided action: do nothing, push to main (falling
// to salvage if the push is rejected), or salvage.
func (r *Runner) executeReap(ctx context.Context, work string, env reapEnv, action reapAction, findings []scanFinding, status string) error {
	switch action {
	case reapNothing:
		fmt.Fprintln(os.Stderr, "ward container reap: nothing to reap")
		return nil
	case reapPushMain:
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
		return r.salvage(ctx, work, env, reason, authCause, findings, status)
	case reapSalvage:
		reason := reasonConflict
		if len(findings) > 0 {
			reason = reasonScan
		}
		return r.salvage(ctx, work, env, reason, false, findings, status)
	}
	return nil
}

// salvage preserves residual work on a ward-salvage/<id> branch (durable) then
// best-effort files/appends a forgejo issue (notification); the branch goes first.
func (r *Runner) salvage(ctx context.Context, work string, env reapEnv, reason reapReason, authCause bool, findings []scanFinding, status string) error {
	id := env.Name + "-" + randHex()
	branch := salvageBranchName(id)
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

	age, _ := formatTokenAge(env.UpAt, time.Now())
	report := salvageReport{
		Repo:      env.repo(),
		Mode:      env.Mode,
		Branch:    branch,
		Reason:    reason,
		AuthCause: authCause,
		TokenAge:  age,
		Findings:  findings,
		Status:    status,
		Base:      env.Base,
	}
	if ferr := r.fileSalvageIssue(ctx, env, report); ferr != nil {
		// The branch already preserved the work; a failed issue is a missed
		// notification, not lost work. Log loudly and succeed.
		fmt.Fprintf(os.Stderr, "ward container reap: filed branch but could not file issue: %v\n", ferr)
	}
	return nil
}

// fileSalvageIssue appends to an open salvage issue for the repo if one exists,
// else opens one. Uses the container FORGEJO_TOKEN directly (no SSM/--aws).
func (r *Runner) fileSalvageIssue(ctx context.Context, env reapEnv, report salvageReport) error {
	if env.Token == "" {
		return fmt.Errorf("no FORGEJO_TOKEN to file a salvage issue")
	}
	// The ops mount authenticates from $FORGEJO_TOKEN inside a container (via
	// forgejoTokenResolver), so the reaper drives the same client host flows do.
	fc, err := r.hostForgejoClient(ctx)
	if err != nil {
		return err
	}
	fc = fc.withMode(containerMode(env.Mode))
	body := salvageIssueBody(report)
	if n, found, err := fc.findOpenIssueByTitlePrefix(ctx, env.Owner, env.Name, salvageIssueTitlePrefix); err == nil && found {
		fmt.Fprintf(os.Stderr, "ward container reap: appending to open salvage issue #%d\n", n)
		return fc.commentIssue(ctx, env.Owner, env.Name, n, body)
	} else if err != nil {
		return err
	}
	n, err := fc.createIssue(ctx, env.Owner, env.Name, salvageIssueTitle(report), body)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ward container reap: filed salvage issue #%d\n", n)
	return nil
}

// releaseReservationIfUnstarted retracts the remote issue reservation on a clean
// reap when the agent never launched (ward#264, docs/agent-reservation.md).
func (r *Runner) releaseReservationIfUnstarted(ctx context.Context, env reapEnv) {
	if !env.reservationReleasable() {
		return
	}
	if env.Token == "" {
		fmt.Fprintln(os.Stderr, "ward container reap: no FORGEJO_TOKEN to release the issue reservation")
		return
	}
	fc, err := r.hostForgejoClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not build forgejo client to release reservation: %v\n", err)
		return
	}
	fc = fc.withMode(containerMode(env.Mode))
	body := reservationReleaseCommentBody(containerMode(env.Mode), env.Name)
	if err := fc.commentIssue(ctx, env.Owner, env.Name, env.Issue, body); err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not release issue reservation on #%d: %v\n", env.Issue, err)
		return
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

// checkExtraRepoLanded reports whether a grant's local HEAD is contained in its
// freshly-fetched origin/main; un-landed work is committed + preserved first.
func (r *Runner) checkExtraRepoLanded(ctx context.Context, env reapEnv, repo targetRepo, work string) (extraRepoUnlanded, bool) {
	status := r.captureAndCommitResidualRepo(ctx, work, env.Mode, repo.slug())
	_ = r.Runner.Exec(ctx, "git", "-C", work, "fetch", "origin")
	rep := extraRepoUnlanded{Repo: repo, Status: status}
	if !refExists(ctx, r, work, "origin/main") {
		// No remote main to compare against: we cannot prove the work landed, so
		// treat it as un-landed and preserve whatever HEAD holds.
		rep.NoMain = true
		r.preserveExtraRepo(ctx, work, env, &rep)
		return rep, false
	}
	ahead := revCount(ctx, r, work, "origin/main..HEAD")
	if ahead == 0 {
		// HEAD is contained in the freshly-fetched remote main: the push landed.
		return extraRepoUnlanded{}, true
	}
	rep.Ahead = ahead
	r.preserveExtraRepo(ctx, work, env, &rep)
	return rep, false
}

// preserveExtraRepo pushes a granted repo's un-landed work to a salvage branch so
// it survives teardown; a push failure falls back to dumping the patch to the log.
func (r *Runner) preserveExtraRepo(ctx context.Context, work string, env reapEnv, rep *extraRepoUnlanded) {
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
	fc, err := r.hostForgejoClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container reap: could not build forgejo client to flag un-landed granted repos: %v\n", err)
		return
	}
	fc = fc.withMode(containerMode(env.Mode))
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
func (r *Runner) diffEntries(ctx context.Context, work, rangeRef string) []diffEntry {
	out, err := r.Runner.Capture(ctx, "git", "-C", work, "diff", "--no-renames", "--numstat", rangeRef)
	if err != nil {
		return nil
	}
	var entries []diffEntry
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := fields[2]
		e := diffEntry{Path: path, Binary: fields[0] == "-" && fields[1] == "-"}
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
