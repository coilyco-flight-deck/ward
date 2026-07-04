package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
)

// agent_reserve.go gives every `ward agent` run a two-sided reservation (local
// sentinel + Forgejo marker comment), TTL-bounded, --force overrides. See docs/agent.md.

// reservationConflictError marks a refusal because another run already holds the issue's
// reservation - not a launch failure, so the director defers it (ward#352).
type reservationConflictError struct{ msg string }

func (e *reservationConflictError) Error() string { return e.msg }

// Code and Kind make the conflict a cli-guard exitcode.Coded error so the CLI exits
// dispatchReservationConflict; isReservationConflict still recovers it (ward#485).
func (e *reservationConflictError) Code() int    { return dispatchReservationConflict }
func (e *reservationConflictError) Kind() string { return "reservation_conflict" }

// newReservationConflict builds a typed conflict refusal with a formatted message, so a
// caller can recover the intent with isReservationConflict regardless of the prose.
func newReservationConflict(format string, args ...any) error {
	return &reservationConflictError{msg: fmt.Sprintf(format, args...)}
}

// isReservationConflict reports whether err is (or wraps) a reservation-conflict refusal
// - "another run beat me to it" - vs a genuine launch failure (ward#352).
func isReservationConflict(err error) bool {
	var rc *reservationConflictError
	return errors.As(err, &rc)
}

// agentReservationsSubdir is the directory under ~/.ward holding one sentinel
// file per reserved issue.
const agentReservationsSubdir = "agent-reservations"

// agentReservationTTL bounds how long a reservation blocks a fresh run. A run
// that outlives it is assumed dead; the reservation goes stale and is reclaimed.
const agentReservationTTL = 2 * time.Hour

// reservationRecheckDefaultMax is the ceiling of the jittered re-check window: a run
// waits a random [max/3,max] before re-reading (ward#600, docs/agent-reservation.md).
const reservationRecheckDefaultMax = 15 * time.Second

// reservationRecheckEnv overrides the ceiling (min derives as max/3); "0"/"off"/"none"
// disables the re-check, leaning on the broker per-ref lock + pre-post check alone.
const reservationRecheckEnv = "WARD_AGENT_RESERVE_RECHECK"

// reservationRecheckSleep is the re-check backoff wait, a package var so a test
// stubs out the real sleep.
var reservationRecheckSleep = time.Sleep

// reservationRecheckDelay picks this run's randomized wait in [max/3, max]; a var so a
// test forces a deterministic value (or 0 to disable). math/rand/v2 is auto-seeded.
var reservationRecheckDelay = func() time.Duration {
	hi := reservationRecheckMax()
	if hi <= 0 {
		return 0
	}
	lo := hi / 3
	if hi <= lo {
		return hi
	}
	return lo + time.Duration(rand.Int64N(int64(hi-lo))) //nolint:gosec // symmetry-breaking jitter, not a security primitive - a predictable value is fine
}

// reservationRecheckMax resolves the re-check ceiling from the env override, else
// the default; "0"/"off"/"none"/"false" (or an unparseable/negative value) disables it.
func reservationRecheckMax() time.Duration {
	v := strings.TrimSpace(os.Getenv(reservationRecheckEnv))
	if v == "" {
		return reservationRecheckDefaultMax
	}
	switch strings.ToLower(v) {
	case "0", "off", "none", "false":
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return reservationRecheckDefaultMax
	}
	return d
}

// agentReservationMarker is the hidden token embedded in every reservation
// comment so the remote check can recognize one regardless of its prose.
const agentReservationMarker = "<!-- ward-agent-reservation -->"

// agentReservationReleaseMarker is the token a release comment carries to retract
// a reservation a pre-launch-death container took (ward#264, docs/agent-reservation.md).
const agentReservationReleaseMarker = "<!-- ward-agent-reservation-released -->"

// agentReservation is the local sentinel payload: who holds an issue, in which
// container, since when. Persisted as pretty JSON so a human can read it.
type agentReservation struct {
	Owner     string    `json:"owner"`
	Repo      string    `json:"repo"`
	Number    int       `json:"number"`
	Mode      string    `json:"mode"`
	Container string    `json:"container"`
	Branch    string    `json:"branch"`
	Host      string    `json:"host"`
	PID       int       `json:"pid"`
	At        time.Time `json:"at"`
}

// summary renders a reservation for a refusal message.
func (res agentReservation) summary() string {
	c := res.Container
	if c == "" {
		c = "(unknown container)"
	}
	host := res.Host
	if host == "" {
		host = "(unknown host)"
	}
	return fmt.Sprintf("container %s on host %s (since %s)", c, host, res.At.UTC().Format(time.RFC3339))
}

// agentReservationFilename is the per-issue sentinel basename, slugged so it is
// filesystem-safe and collision-free across owners/repos.
func agentReservationFilename(ref agentIssueRef) string {
	return config.SanitizeSlug(fmt.Sprintf("%s-%s-issue-%d", ref.Owner, ref.Repo, ref.Number)) + ".json"
}

// agentReservationPath resolves ~/.ward/agent-reservations/<slug>.json for ref.
func agentReservationPath(ref agentIssueRef) (string, error) {
	dir, err := config.GlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, agentReservationsSubdir, agentReservationFilename(ref)), nil
}

// reservationFresh reports whether a reservation stamped at `at` still blocks as
// of `now`. A zero or future-skewed stamp is conservatively treated as fresh.
func reservationFresh(at, now time.Time, ttl time.Duration) bool {
	if at.IsZero() {
		return false
	}
	return now.Sub(at) < ttl
}

// readAgentReservation loads the sentinel at path. A missing file is (nil,
// false, nil); a corrupt one is treated as absent so it can't wedge the issue.
func readAgentReservation(path string) (*agentReservation, bool, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- path is ward-derived under ~/.ward
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var res agentReservation
	if err := json.Unmarshal(b, &res); err != nil {
		//nolint:nilerr // a corrupt sentinel is treated as absent so it can't wedge the issue
		return nil, false, nil
	}
	return &res, true, nil
}

// writeAgentReservation persists res to path atomically (write-temp-then-rename)
// at 0600, creating the reservations dir as needed.
func writeAgentReservation(path string, res agentReservation) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// removeAgentReservation deletes the sentinel, tolerating an already-gone file.
func removeAgentReservation(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// hostname is the best-effort host identifier baked into a reservation.
func hostname() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return h
	}
	return "unknown"
}

// reserveIssue acquires both reservation sides before a container fires (ward#570). It
// returns a release retracting both halves; a remote failure rolls the local hold back.
func (r *Runner) reserveIssue(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container, branch, justification string, force bool) (func(), error) {
	now := time.Now().UTC()
	fmt.Fprintf(os.Stderr, "%s: reservation start for %s (container=%s branch=%s force=%t)\n", label, ref, container, branch, force)
	releaseLocal, err := r.acquireLocalReservation(ctx, label, mode, ref, container, branch, now, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: reservation local acquire failed for %s: %v\n", label, ref, err)
		return nil, err
	}
	releaseRemote, err := r.acquireRemoteReservation(ctx, label, mode, ref, container, justification, now, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: reservation remote acquire failed for %s: %v\n", label, ref, err)
		releaseLocal()
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "%s: reservation acquired for %s\n", label, ref)
	return func() {
		if releaseRemote != nil {
			releaseRemote()
		}
		releaseLocal()
	}, nil
}

// precheckReservation runs reserveIssue's read-only refusal half ahead of the LLM
// pre-flight (ward#184), reusing the fetched thread; --force bypasses it. See docs.
func (r *Runner) precheckReservation(ctx context.Context, label string, w resolvedWork, force bool) error {
	fmt.Fprintf(os.Stderr, "%s: reservation precheck start for %s (force=%t)\n", label, w.Ref, force)
	if force {
		fmt.Fprintf(os.Stderr, "%s: reservation precheck skipped for %s because force is set\n", label, w.Ref)
		return nil
	}
	now := time.Now().UTC()
	if err := r.precheckLocalReservation(ctx, label, w.Ref, now); err != nil {
		fmt.Fprintf(os.Stderr, "%s: reservation precheck failed locally for %s: %v\n", label, w.Ref, err)
		return err
	}
	if who, held := freshReservationComment(w.Comments, now, agentReservationTTL); held {
		fmt.Fprintf(os.Stderr, "%s: reservation precheck failed remotely for %s: %s\n", label, w.Ref, who)
		return newReservationConflict(
			"%s: issue %s is already reserved remotely (%s); wait for it to finish or pass --force to override",
			label, w.Ref, who)
	}
	fmt.Fprintf(os.Stderr, "%s: reservation precheck passed for %s\n", label, w.Ref)
	return nil
}

// precheckLocalReservation reports a conflict if a fresh local sentinel for a
// still-running container owns ref, without writing one (shared, ward#184).
func (r *Runner) precheckLocalReservation(ctx context.Context, label string, ref agentIssueRef, now time.Time) error {
	path, err := agentReservationPath(ref)
	if err != nil {
		return fmt.Errorf("%s: resolve reservation path: %w", label, err)
	}
	existing, ok, rerr := readAgentReservation(path)
	if rerr != nil {
		return fmt.Errorf("%s: read reservation %s: %w", label, path, rerr)
	}
	if ok && reservationFresh(existing.At, now, agentReservationTTL) && r.containerRunning(ctx, existing.Container) {
		fmt.Fprintf(os.Stderr, "%s: reservation precheck saw live local holder for %s at %s\n", label, ref, path)
		return newReservationConflict(
			"%s: issue %s is already reserved locally by %s; wait for it to finish or pass --force to reclaim",
			label, ref, existing.summary())
	}
	fmt.Fprintf(os.Stderr, "%s: reservation precheck found no live local holder for %s\n", label, ref)
	return nil
}

// acquireLocalReservation writes this run's sentinel (refusing a fresh one held by a
// still-running container unless force) and returns a release that deletes it.
func (r *Runner) acquireLocalReservation(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container, branch string, now time.Time, force bool) (func(), error) {
	path, err := agentReservationPath(ref)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve reservation path: %w", label, err)
	}
	if !force {
		if err := r.precheckLocalReservation(ctx, label, ref, now); err != nil {
			return nil, err
		}
	}
	fmt.Fprintf(os.Stderr, "%s: reservation local acquire writing %s\n", label, path)
	res := agentReservation{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Mode:      string(mode),
		Container: container,
		Branch:    branch,
		Host:      hostname(),
		PID:       os.Getpid(),
		At:        now,
	}
	if err := writeAgentReservation(path, res); err != nil {
		return nil, fmt.Errorf("%s: write reservation %s: %w", label, path, err)
	}
	fmt.Fprintf(os.Stderr, "%s: reservation local acquire wrote %s\n", label, path)
	return func() { _ = removeAgentReservation(path) }, nil
}

// containerRunning reports whether the named container is running; an empty name or
// indeterminate docker error is treated as "running" to avoid false-negative reclaims.
func (r *Runner) containerRunning(ctx context.Context, name string) bool {
	if strings.TrimSpace(name) == "" {
		return true
	}
	out, err := r.dockerCapture(ctx, "ps",
		"--filter", "name=^"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != ""
}

// remoteReservationPostAttempts / remoteReservationPostBackoff bound the best-effort
// retry on the reservation-comment post - it rides a transient failure (ward#402).
const (
	remoteReservationPostAttempts = 3
	remoteReservationPostBackoff  = 2 * time.Second
)

// reservationPostSleep is the backoff wait between reservation-post retries, a
// package var so a test stubs the real sleep out.
var reservationPostSleep = time.Sleep

// reservationWarnToken is the stable, greppable substring every dropped-reservation
// WARN carries, so an operator sweeps the dispatch logs by grep (ward#402).
const reservationWarnToken = "remote reservation NOT posted"

// acquireRemoteReservation refuses on a fresh reservation comment (unless force), else
// posts one and returns a remote-release to roll it back (ward#402/#570, docs).
func (r *Runner) acquireRemoteReservation(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container, justification string, now time.Time, force bool) (func(), error) {
	cl, err := r.hostForgeClient(ctx, ref.Forge, mode)
	if err != nil {
		warnRemoteReservationLost(label, ref, fmt.Sprintf("could not build the %s client: %v", ref.Forge, err))
		return nil, nil
	}
	if !force {
		comments, lerr := cl.listIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "%s: warning: could not read issue comments to check for a remote reservation (%v); proceeding without the cross-host conflict check\n", label, lerr)
		} else if who, held := freshReservationComment(comments, now, agentReservationTTL); held {
			return nil, newReservationConflict(
				"%s: issue %s is already reserved remotely (%s); wait for it to finish or pass --force to override",
				label, ref, who)
		}
	}
	tries, perr := postReservationComment(ctx, remoteReservationPostAttempts, remoteReservationPostBackoff, reservationPostSleep,
		func(ctx context.Context) error {
			return cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number,
				reservationCommentBody(mode, container, hostname(), now, justification))
		})
	if perr != nil {
		warnRemoteReservationLost(label, ref, fmt.Sprintf("post failed after %d attempt(s): %v", tries, perr))
	}
	// Seal the conversation against in-flight steering (ward#494); best-effort, so a
	// forge with no lock API or a lock failure never fails the reservation.
	r.lockReservedIssue(ctx, cl, label, ref)
	// Jittered double-check backstop for any path the broker per-ref lock misses
	// (two direct host runs, or cross-host); --force skips it (ward#600, docs).
	if !force {
		if lost, winner := r.reservationRecheckLost(ctx, cl, label, ref, container); lost {
			// Leave our comment to lapse at the TTL (a release marker would free the
			// winner too) and refuse so only the winner proceeds (ward#600).
			return nil, newReservationConflict(
				"%s: issue %s went to a concurrent run (%s) on a jittered re-check; this run yields (ward#600)",
				label, ref, winner)
		}
	}
	// The release reuses this same client to retract the comment + lock if the launch
	// fails downstream (ward#570).
	return func() { r.releaseRemoteReservation(ctx, cl, label, mode, ref, container) }, nil
}

// releaseRemoteReservation retracts this run's forge road-block on a launch that dies
// before the container is up: a release-marker comment plus a best-effort unlock (#570).
func (r *Runner) releaseRemoteReservation(ctx context.Context, cl issueForge, label string, mode containerMode, ref agentIssueRef, container string) {
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, reservationReleaseCommentBody(mode, container)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: warning: could not release the remote reservation on %s (%v); a re-run may need --force until the %s TTL lapses (ward#570)\n", label, ref, err, agentReservationTTL)
		return
	}
	// Retract the reservation's conversation lock (ward#494) so a retry lands on an open
	// thread; silent on the no-lock-leaf forge (Forgejo today).
	if err := cl.unlockIssue(ctx, ref.Owner, ref.Repo, ref.Number); err != nil && !errors.Is(err, errForgeLockUnsupported) {
		fmt.Fprintf(os.Stderr, "%s: warning: could not unlock %s after releasing the reservation (%v) (ward#570)\n", label, ref, err)
	}
	fmt.Fprintf(os.Stderr, "%s: released remote reservation on %s (launch failed before the container came up, ward#570)\n", label, ref)
}

// lockReservedIssue seals the reserved issue best-effort, logging the outcome (locked
// / unsupported-forge / soft-failure) and never returning an error (ward#494, docs).
func (r *Runner) lockReservedIssue(ctx context.Context, cl issueForge, label string, ref agentIssueRef) {
	switch err := cl.lockIssue(ctx, ref.Owner, ref.Repo, ref.Number); {
	case err == nil:
		fmt.Fprintf(os.Stderr, "%s: locked issue %s conversation for the reservation window\n", label, ref)
	case errors.Is(err, errForgeLockUnsupported):
		fmt.Fprintf(os.Stderr, "%s: issue %s conversation left unlocked - the %s API has no lock leaf; the reservation comment is the road-block (ward#494)\n", label, ref, ref.Forge)
	default:
		fmt.Fprintf(os.Stderr, "%s: warning: could not lock issue %s conversation (%v); the reservation comment still stands as the road-block (ward#494)\n", label, ref, err)
	}
}

// postReservationComment runs post up to attempts times, sleeping backoff between tries
// (never after the last), returning the attempt count + final error (nil on success).
func postReservationComment(ctx context.Context, attempts int, backoff time.Duration, sleep func(time.Duration), post func(context.Context) error) (int, error) {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = post(ctx); err == nil {
			return attempt, nil
		}
		if attempt < attempts {
			sleep(backoff)
		}
	}
	return attempts, err
}

// warnRemoteReservationLost prints the loud, greppable WARN for a run whose remote
// reservation could not be posted; the run proceeds on the local sentinel (ward#402).
func warnRemoteReservationLost(label string, ref agentIssueRef, detail string) {
	fmt.Fprintf(os.Stderr,
		"%s: warning: %s for %s (%s); the local sentinel still holds this host, but cross-host dedup and "+
			"the issue-thread reservation signal are LOST for this run - check the host forgejo token/SSM "+
			"path and this issue's thread (ward#402)\n",
		label, reservationWarnToken, ref, detail)
}

// freshReservationComment describes the still-blocking reservation on the thread,
// if any: the latest fresh reservation with no release (ward#264) at/after it.
func freshReservationComment(comments []issueComment, now time.Time, ttl time.Duration) (string, bool) {
	var latest *issueComment
	var released time.Time
	for i := range comments {
		c := &comments[i]
		// Test release first: its marker is a distinct substring, but ordering the
		// check keeps the intent obvious.
		if strings.Contains(c.Body, agentReservationReleaseMarker) {
			if c.CreatedAt.After(released) {
				released = c.CreatedAt
			}
			continue
		}
		if !strings.Contains(c.Body, agentReservationMarker) {
			continue
		}
		if !reservationFresh(c.CreatedAt, now, ttl) {
			continue
		}
		if latest == nil || c.CreatedAt.After(latest.CreatedAt) {
			latest = c
		}
	}
	if latest == nil {
		return "", false
	}
	// A release stamped at or after the latest reservation retracts it.
	if !released.Before(latest.CreatedAt) {
		return "", false
	}
	who := latest.User.Login
	if who == "" {
		who = "another run"
	}
	return fmt.Sprintf("by @%s at %s", who, latest.CreatedAt.UTC().Format(time.RFC3339)), true
}

// reservationClaimRE pulls the "container `X` on host `Y`" identity out of a
// reservation comment body for the re-check tiebreak (ward#600).
var reservationClaimRE = regexp.MustCompile("container `([^`]+)` on host `([^`]+)`")

// reservationClaim is one live reservation on the thread: when it was posted and
// who posted it (container@host), the two keys the deterministic tiebreak sorts on.
type reservationClaim struct {
	at       time.Time
	identity string
}

// reservationRecheckLost re-reads the thread after a jittered pause; a concurrent run
// winning the tiebreak makes this run yield. A disabled window/read error fails open.
func (r *Runner) reservationRecheckLost(ctx context.Context, cl issueForge, label string, ref agentIssueRef, container string) (bool, string) {
	delay := reservationRecheckDelay()
	if delay <= 0 {
		return false, "" // disabled via WARD_AGENT_RESERVE_RECHECK
	}
	ours := container + "@" + hostname()
	fmt.Fprintf(os.Stderr, "%s: reservation re-check waiting %s before confirming %s (ward#600)\n", label, delay.Round(time.Millisecond), ref)
	reservationRecheckSleep(delay)
	now := time.Now().UTC()
	comments, err := cl.listIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: warning: reservation re-check could not re-read %s (%v); proceeding (ward#600)\n", label, ref, err)
		return false, ""
	}
	winner, ok := winningReservationClaim(reservationClaims(comments, now, agentReservationTTL))
	if !ok || winner.identity == ours {
		fmt.Fprintf(os.Stderr, "%s: reservation re-check confirmed %s for this run (ward#600)\n", label, ref)
		return false, ""
	}
	fmt.Fprintf(os.Stderr, "%s: reservation re-check yields %s to %s (ward#600)\n", label, ref, winner.identity)
	return true, winner.identity
}

// reservationClaims extracts the live (fresh, un-released) reservation claims from the
// thread, honoring freshReservationComment's release semantics (newest release wins).
func reservationClaims(comments []issueComment, now time.Time, ttl time.Duration) []reservationClaim {
	var released time.Time
	for i := range comments {
		if strings.Contains(comments[i].Body, agentReservationReleaseMarker) && comments[i].CreatedAt.After(released) {
			released = comments[i].CreatedAt
		}
	}
	var claims []reservationClaim
	for i := range comments {
		c := &comments[i]
		if strings.Contains(c.Body, agentReservationReleaseMarker) || !strings.Contains(c.Body, agentReservationMarker) {
			continue
		}
		if !reservationFresh(c.CreatedAt, now, ttl) {
			continue
		}
		// A release stamped at or after this claim retracts it.
		if !released.Before(c.CreatedAt) {
			continue
		}
		claims = append(claims, reservationClaim{at: c.CreatedAt, identity: reservationClaimIdentity(*c)})
	}
	return claims
}

// reservationClaimIdentity is the tiebreak key: container@host parsed from the
// marker body, falling back to the commenter login so the key is never empty.
func reservationClaimIdentity(c issueComment) string {
	if m := reservationClaimRE.FindStringSubmatch(c.Body); m != nil {
		return m[1] + "@" + m[2]
	}
	return "@" + c.User.Login
}

// winningReservationClaim is the claim that proceeds: earliest timestamp, ties broken
// by lexically-min identity - a total order, so exactly one racer wins (ward#600).
func winningReservationClaim(claims []reservationClaim) (reservationClaim, bool) {
	if len(claims) == 0 {
		return reservationClaim{}, false
	}
	best := claims[0]
	for _, c := range claims[1:] {
		if c.at.Before(best.at) || (c.at.Equal(best.at) && c.identity < best.identity) {
			best = c
		}
	}
	return best, true
}

// reservationCommentBody is the marker comment a run posts to claim an issue. A
// non-empty justification is folded in as the pre-flight's GO read (ward#383).
func reservationCommentBody(mode containerMode, container, host string, now time.Time, justification string) string {
	body := fmt.Sprintf(
		"%s\n🔒 Reserved by `ward agent --driver %s` — container `%s` on host `%s` is carrying this issue (reserved %s). "+
			"Concurrent `ward agent` runs are blocked until it finishes or the reservation goes stale (%s TTL); "+
			"`--force` overrides.\n\n"+
			"**Do not comment on or edit this issue to steer the run while it is reserved.** The engineer seeded "+
			"the body once at launch and never re-reads it, so a comment or edit reaches only human readers, never "+
			"the running engineer. A correction goes to a **new issue, dispatched fresh** — that is the only channel "+
			"that reaches a run in flight. Where the forge supports it, ward locks this conversation to make that a "+
			"road-block rather than a convention (ward#494).",
		agentReservationMarker, mode, container, host, now.Format(time.RFC3339), agentReservationTTL)
	if justification = strings.TrimSpace(justification); justification != "" {
		body += fmt.Sprintf(
			"\n\nThe pre-flight judged this issue **GO** for an unattended run. Its justification:\n\n"+
				"<details><summary>pre-flight read (GO)</summary>\n\n%s\n\n</details>\n",
			justification)
	}
	return body
}

// reservationReleaseCommentBody is the marker comment the reaper posts to retract
// a reservation whose container exited having done nothing (ward#264).
func reservationReleaseCommentBody(mode containerMode, container string) string {
	return fmt.Sprintf(
		"%s\n🔓 Reservation released by `ward container reap` — container `%s` (`--driver %s`) exited without "+
			"launching the agent (smoke-test death, ward#222/#264), so the hold it took is retracted. "+
			"A plain `ward agent` retry no longer needs `--force`.",
		agentReservationReleaseMarker, container, mode)
}
