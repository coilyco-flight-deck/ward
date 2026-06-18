package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
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
	// A dev/source build has no meaningful "latest release" to chase, and the
	// brew-upgrade path doesn't apply to it - skip before touching the network.
	if !versionLooksReleased(Version) {
		return
	}
	latest, ok := fetchLatestWardTag(ctx)
	if !ok {
		return
	}
	if !versionBehind(Version, latest) {
		return
	}
	w := io.Writer(os.Stderr)
	if r != nil && r.Runner != nil && r.Runner.Stderr != nil {
		w = r.Runner.Stderr
	}
	fmt.Fprint(w, wardOutdatedNotice(Version, latest))
}

// wardOutdatedNotice is the two-line stderr reminder, kept pure so it is
// testable without a network or a real release.
func wardOutdatedNotice(current, latest string) string {
	return fmt.Sprintf(
		"ward agent: heads up - your ward %s is behind the latest release %s.\n"+
			"ward agent: this host binary is what dispatches agents; run `ward upgrade` to refresh it.\n",
		current, latest)
}

// versionLooksReleased reports whether Version is a real release tag (not the
// "dev" default or a blank/source build) worth comparing against a release.
func versionLooksReleased(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && v != "dev"
}

// fetchLatestWardTag GETs the latest ward release tag from Forgejo, best-effort and
// unauthenticated. ok is false on any failure so the caller stays silent.
func fetchLatestWardTag(ctx context.Context) (string, bool) {
	cctx, cancel := context.WithTimeout(ctx, wardReleaseCheckTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/api/v1/repos/%s/releases/latest",
		strings.TrimRight(forgejoBaseURL, "/"), wardReleaseRepo)
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", false
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", false
	}
	tag := strings.TrimSpace(rel.TagName)
	if tag == "" {
		return "", false
	}
	return tag, true
}

// versionBehind reports whether current is an older release than latest. A dev build,
// an unparseable tag, or current >= latest all return false (only fires when confident).
func versionBehind(current, latest string) bool {
	if !versionLooksReleased(current) {
		return false
	}
	cur, ok1 := parseSemver(current)
	lat, ok2 := parseSemver(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if cur[i] != lat[i] {
			return cur[i] < lat[i]
		}
	}
	return false
}

// parseSemver splits a vX.Y.Z tag into 3 numeric parts, tolerating a missing v, short
// tags (zero-padded), and -pre/+build suffixes. ok is false on a non-numeric component.
func parseSemver(tag string) (parts [3]int, ok bool) {
	s := strings.TrimSpace(tag)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return parts, false
	}
	// Drop a -prerelease / +build suffix so v0.5.2-rc1 compares as 0.5.2.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	segs := strings.Split(s, ".")
	if len(segs) > 3 {
		segs = segs[:3]
	}
	for i, seg := range segs {
		n, err := strconv.Atoi(seg)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		parts[i] = n
	}
	return parts, true
}
