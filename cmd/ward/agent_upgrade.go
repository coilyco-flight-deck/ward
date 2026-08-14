package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/version"
)

// agent_upgrade.go re-surfaces the "host ward is behind latest" reminder at `ward
// agent` dispatch, since a detached run logs its version unseen. See ward#143.

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
	// A dev/source build has no meaningful "latest release" to chase - skip
	// before touching the network.
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
			"ward agent: this host binary is what dispatches agents; refresh it before launching a long-lived run.\n",
		current, latest)
}

// fetchLatestWardTag resolves the newest ward release tag through the in-binary
// native Forgejo release API (ward#172). See docs/agent.md.
func (r *Runner) fetchLatestWardTag(ctx context.Context) (string, bool) {
	if r == nil {
		return "", false
	}

	cctx, cancel := context.WithTimeout(ctx, wardReleaseCheckTimeout)
	defer cancel()

	releases, err := fetchWardBootstrapReleasesPage(cctx, 1)
	if err != nil {
		return "", false
	}
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		if tag := strings.TrimSpace(release.TagName); tag != "" {
			return tag, true
		}
	}
	return "", false
}
