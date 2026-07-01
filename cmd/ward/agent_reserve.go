package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// reserveIssue acquires both reservation sides before a container fires, returning a
// local-sentinel release; a remote-side failure rolls the local hold back.
func (r *Runner) reserveIssue(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container, branch, justification string, force bool) (func(), error) {
	now := time.Now().UTC()
	releaseLocal, err := r.acquireLocalReservation(ctx, label, mode, ref, container, branch, now, force)
	if err != nil {
		return nil, err
	}
	if err := r.acquireRemoteReservation(ctx, label, mode, ref, container, justification, now, force); err != nil {
		releaseLocal()
		return nil, err
	}
	return releaseLocal, nil
}

// precheckReservation runs reserveIssue's read-only refusal half ahead of the LLM
// pre-flight (ward#184), reusing the fetched thread; --force bypasses it. See docs.
func (r *Runner) precheckReservation(ctx context.Context, label string, w resolvedWork, force bool) error {
	if force {
		return nil
	}
	now := time.Now().UTC()
	if err := r.precheckLocalReservation(ctx, label, w.Ref, now); err != nil {
		return err
	}
	if who, held := freshReservationComment(w.Comments, now, agentReservationTTL); held {
		return newReservationConflict(
			"%s: issue %s is already reserved remotely (%s); wait for it to finish or pass --force to override",
			label, w.Ref, who)
	}
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
		return newReservationConflict(
			"%s: issue %s is already reserved locally by %s; wait for it to finish or pass --force to reclaim",
			label, ref, existing.summary())
	}
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
	return func() { _ = removeAgentReservation(path) }, nil
}

// containerRunning reports whether the named container is running; an empty name or
// indeterminate docker error is treated as "running" to avoid false-negative reclaims.
func (r *Runner) containerRunning(ctx context.Context, name string) bool {
	if strings.TrimSpace(name) == "" {
		return true
	}
	out, err := r.Runner.Capture(ctx, "docker", "ps",
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
// posts one - best-effort but no longer silent: retried, then a WARN (ward#402, docs).
func (r *Runner) acquireRemoteReservation(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container, justification string, now time.Time, force bool) error {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		warnRemoteReservationLost(label, ref, fmt.Sprintf("could not build the forgejo client: %v", err))
		return nil
	}
	if !force {
		comments, lerr := cl.listIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "%s: warning: could not read issue comments to check for a remote reservation (%v); proceeding without the cross-host conflict check\n", label, lerr)
		} else if who, held := freshReservationComment(comments, now, agentReservationTTL); held {
			return newReservationConflict(
				"%s: issue %s is already reserved remotely (%s); wait for it to finish or pass --force to override",
				label, ref, who)
		}
	}
	tries, perr := postReservationComment(ctx, remoteReservationPostAttempts, remoteReservationPostBackoff, reservationPostSleep,
		func(ctx context.Context) error {
			return cl.withMode(mode).commentIssue(ctx, ref.Owner, ref.Repo, ref.Number,
				reservationCommentBody(mode, container, hostname(), now, justification))
		})
	if perr != nil {
		warnRemoteReservationLost(label, ref, fmt.Sprintf("post failed after %d attempt(s): %v", tries, perr))
	}
	return nil
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

// warnRemoteReservationLost prints the loud, greppable WARN for a carry whose remote
// reservation could not be posted; the run proceeds on the local sentinel (ward#402).
func warnRemoteReservationLost(label string, ref agentIssueRef, detail string) {
	fmt.Fprintf(os.Stderr,
		"%s: warning: %s for %s (%s); the local sentinel still holds this host, but cross-host dedup and "+
			"the issue-thread reservation signal are LOST for this carry - check the host forgejo token/SSM "+
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

// reservationCommentBody is the marker comment a run posts to claim an issue. A
// non-empty justification is folded in as the pre-flight's GO read (ward#383).
func reservationCommentBody(mode containerMode, container, host string, now time.Time, justification string) string {
	body := fmt.Sprintf(
		"%s\n🔒 Reserved by `ward agent --driver %s` — container `%s` on host `%s` is carrying this issue (reserved %s). "+
			"Concurrent `ward agent` runs are blocked until it finishes or the reservation goes stale (%s TTL); "+
			"`--force` overrides.",
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
