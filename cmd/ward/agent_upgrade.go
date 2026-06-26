package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/version"
)

// agent_upgrade.go re-surfaces the "host ward is behind latest" reminder at `ward
// agent` dispatch, since a detached run logs its version unseen. See ward#143.

// wardReleaseRepo is the repo whose latest release defines "current ward".
const wardReleaseRepo = "coilyco-flight-deck/ward"

// wardReleaseCheckTimeout caps the best-effort release lookup so a slow or
// unreachable Forgejo never holds up an agent dispatch.
const wardReleaseCheckTimeout = 5 * time.Second

// maybeWarnWardOutdated prints a best-effort stderr reminder when the host ward is
// behind latest. It never errors or blocks dispatch: any failure stays quiet.
func (r *Runner) maybeWarnWardOutdated(ctx context.Context) {
	latest, behind := r.wardOutdated(ctx)
	if !behind {
		return
	}
	w := io.Writer(os.Stderr)
	if r != nil && r.Runner != nil && r.Runner.Stderr != nil {
		w = r.Runner.Stderr
	}
	_, _ = fmt.Fprint(w, wardOutdatedNotice(Version, latest))
}

// wardOutdated reports the latest ward release tag and whether the host binary is
// behind it. Best-effort and quiet on every failure mode (the gate + heads-up share it).
func (r *Runner) wardOutdated(ctx context.Context) (latest string, behind bool) {
	// A dev/source build has no meaningful "latest release" to chase, and the
	// brew-upgrade path doesn't apply to it - skip before touching the network.
	if !version.LooksReleased(Version) {
		return "", false
	}
	tag, ok := r.fetchLatestWardTag(ctx)
	if !ok {
		return "", false
	}
	return tag, version.Behind(Version, tag)
}

// wardOutdatedNotice is the two-line stderr reminder, kept pure so it is
// testable without a network or a real release.
func wardOutdatedNotice(current, latest string) string {
	return fmt.Sprintf(
		"ward agent: heads up - your ward %s is behind the latest release %s.\n"+
			"ward agent: this host binary is what dispatches agents; run `ward upgrade` to refresh it.\n",
		current, latest)
}

// fetchLatestWardTag resolves the newest ward release tag through the in-binary
// `ward ops forgejo release list` specverb (ward#172). See docs/agent.md.
func (r *Runner) fetchLatestWardTag(ctx context.Context) (string, bool) {
	if r == nil || r.Runner == nil {
		return "", false
	}
	owner, repo, ok := strings.Cut(wardReleaseRepo, "/")
	if !ok || owner == "" || repo == "" {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}

	cctx, cancel := context.WithTimeout(ctx, wardReleaseCheckTimeout)
	defer cancel()

	// Swallow the specverb's own stderr (SSM miss, upstream error): this nag is
	// silent on failure, so its chatter would only confuse. Stdout is captured.
	prevErr := r.Runner.Stderr
	r.Runner.Stderr = io.Discard
	out, err := r.Runner.Capture(cctx, exe,
		"ops", "forgejo", "release", "list", owner, repo,
		"--draft=false", "--pre-release=false",
		"--query", "[0].tag_name",
		"--output", "text",
	)
	r.Runner.Stderr = prevErr
	if err != nil {
		return "", false
	}
	tag := strings.TrimSpace(string(out))
	if tag == "" {
		return "", false
	}
	return tag, true
}
