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

// agent_reserve.go gives every `ward agent` run a two-sided reservation so a
// second run never works the same issue at the same time:
//
//   - locally, a file sentinel under ~/.ward/agent-reservations names the issue
//     a run is carrying (with the container that holds it), and
//   - remotely, a marker comment on the Forgejo issue advertises the hold to
//     other hosts.
//
// Both are TTL-bounded: a reservation older than agentReservationTTL is treated
// as stale (a crashed or long-dead run shouldn't wedge the issue forever), and
// the local sentinel is additionally reclaimed the moment its container is no
// longer running. --force overrides both checks. See docs/agent.md.

// agentReservationsSubdir is the directory under ~/.ward holding one sentinel
// file per reserved issue.
const agentReservationsSubdir = "agent-reservations"

// agentReservationTTL bounds how long a reservation blocks a fresh run. A run
// that outlives it is assumed dead; the reservation goes stale and is reclaimed.
const agentReservationTTL = 2 * time.Hour

// agentReservationMarker is the hidden token embedded in every reservation
// comment so the remote check can recognize one regardless of its prose.
const agentReservationMarker = "<!-- ward-agent-reservation -->"

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
		//nolint:nilerr // a corrupt sentinel is treated as absent, not a hard error, so it can't wedge the issue
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

// reserveIssue acquires both sides of the reservation before a container fires.
// It returns a release for the local sentinel; the caller decides whether to
// call it (attached runs release on return, detached runs leave it for the TTL).
// A remote-side failure rolls the local hold back so a fixed retry isn't
// self-blocked.
func (r *Runner) reserveIssue(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container, branch string, force bool) (func(), error) {
	now := time.Now().UTC()
	releaseLocal, err := r.acquireLocalReservation(ctx, label, mode, ref, container, branch, now, force)
	if err != nil {
		return nil, err
	}
	if err := r.acquireRemoteReservation(ctx, label, mode, ref, container, now, force); err != nil {
		releaseLocal()
		return nil, err
	}
	return releaseLocal, nil
}

// acquireLocalReservation writes this run's sentinel, refusing if a fresh one is
// already held by a still-running container (unless force). It returns a release
// that deletes the sentinel.
func (r *Runner) acquireLocalReservation(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container, branch string, now time.Time, force bool) (func(), error) {
	path, err := agentReservationPath(ref)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve reservation path: %w", label, err)
	}
	if !force {
		existing, ok, rerr := readAgentReservation(path)
		if rerr != nil {
			return nil, fmt.Errorf("%s: read reservation %s: %w", label, path, rerr)
		}
		if ok && reservationFresh(existing.At, now, agentReservationTTL) && r.containerRunning(ctx, existing.Container) {
			return nil, fmt.Errorf(
				"%s: issue %s is already reserved locally by %s; wait for it to finish or pass --force to reclaim",
				label, ref, existing.summary())
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

// containerRunning reports whether a container with the given name is currently
// running. An empty name or an indeterminate docker error is treated as "still
// running" so a reservation is never reclaimed on a false negative.
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

// acquireRemoteReservation refuses when a fresh reservation comment already sits
// on the issue (unless force), otherwise posts one. Network/auth failures
// degrade to a warning - the local sentinel still guards this host - so a
// transient Forgejo hiccup never blocks a launch.
func (r *Runner) acquireRemoteReservation(ctx context.Context, label string, mode containerMode, ref agentIssueRef, container string, now time.Time, force bool) error {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: skipping remote reservation (%v); the local sentinel still holds\n", label, err)
		return nil
	}
	if !force {
		comments, lerr := cl.listIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "%s: note: could not read issue comments to check for a remote reservation (%v); proceeding\n", label, lerr)
		} else if who, held := freshReservationComment(comments, now, agentReservationTTL); held {
			return fmt.Errorf(
				"%s: issue %s is already reserved remotely (%s); wait for it to finish or pass --force to override",
				label, ref, who)
		}
	}
	if err := cl.commentIssue(ctx, ref.Owner, ref.Repo, ref.Number, reservationCommentBody(mode, container, hostname(), now)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: note: could not post the remote reservation comment (%v); the local sentinel still holds\n", label, err)
	}
	return nil
}

// freshReservationComment returns a description of the most recent still-fresh
// reservation comment, if any, and whether one was found.
func freshReservationComment(comments []issueComment, now time.Time, ttl time.Duration) (string, bool) {
	for _, c := range comments {
		if !strings.Contains(c.Body, agentReservationMarker) {
			continue
		}
		if !reservationFresh(c.CreatedAt, now, ttl) {
			continue
		}
		who := c.User.Login
		if who == "" {
			who = "another run"
		}
		return fmt.Sprintf("by @%s at %s", who, c.CreatedAt.UTC().Format(time.RFC3339)), true
	}
	return "", false
}

// reservationCommentBody is the marker comment a run posts to claim an issue.
func reservationCommentBody(mode containerMode, container, host string, now time.Time) string {
	return fmt.Sprintf(
		"%s\n🔒 Reserved by `ward agent %s` — container `%s` on host `%s` is carrying this issue (reserved %s). "+
			"Concurrent `ward agent` runs are blocked until it finishes or the reservation goes stale (%s TTL); "+
			"`--force` overrides.",
		agentReservationMarker, mode, container, host, now.Format(time.RFC3339), agentReservationTTL)
}
