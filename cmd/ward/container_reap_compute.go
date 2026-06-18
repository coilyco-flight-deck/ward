package main

// container_reap_compute.go holds the pure decision logic behind `ward
// container reap` (side effects live in container_reap.go). See docs/container-reap.md.

import (
	"fmt"
	"sort"
	"strings"
	"time"
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
	reasonPushRace reapReason = "push to main was rejected (the remote advanced)"
	reasonPushFail reapReason = "push to main failed"
	reasonAuthFail reapReason = "push to main was rejected on auth (dead or rotated PAT)"
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
	Findings []scanFinding
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

// --- junk scan ---------------------------------------------------------------

// diffEntry is one path in the residual diff, with enough metadata for the
// scan: its size and whether git treated it as binary.
type diffEntry struct {
	Path   string
	Bytes  int64
	Binary bool
}

// scanFinding is one reason a path should not auto-land on main.
type scanFinding struct {
	Path   string
	Reason string
}

const (
	// oversizedBlobBytes flags any file this large or larger - almost always a
	// build artifact or a vendored binary rather than authored source.
	oversizedBlobBytes int64 = 5 << 20 // 5 MiB
	// binaryBlobBytes flags binaries at a lower bar than text, since a large
	// committed binary is rarely intended.
	binaryBlobBytes int64 = 1 << 20 // 1 MiB
)

// vendoredDirs mark machine-generated or fetched trees. High-confidence names
// only; ambiguous source-or-build names (build, dist, out) are omitted.
var vendoredDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
	".next":        true,
	".terraform":   true,
	".gradle":      true,
	"target":       true,
}

// secretBasenames are filenames that are credential material by convention.
var secretBasenames = map[string]bool{
	".env":             true,
	".git-credentials": true,
	".netrc":           true,
	".pgpass":          true,
	"credentials":      true,
	"id_rsa":           true,
	"id_dsa":           true,
	"id_ecdsa":         true,
	"id_ed25519":       true,
}

// secretSuffixes are extensions that are key material by convention.
var secretSuffixes = []string{".pem", ".key", ".p12", ".pfx", ".keystore", ".jks"}

// scanDiff flags paths that should not silently land on main: vendored trees,
// credential-shaped files, oversized/large-binary blobs. First match per path wins.
func scanDiff(entries []diffEntry) []scanFinding {
	var out []scanFinding
	for _, e := range entries {
		if seg, ok := vendoredSegment(e.Path); ok {
			out = append(out, scanFinding{e.Path, "vendored/generated tree (" + seg + "/)"})
			continue
		}
		if reason, ok := secretLikePath(e.Path); ok {
			out = append(out, scanFinding{e.Path, reason})
			continue
		}
		if e.Binary && e.Bytes >= binaryBlobBytes {
			out = append(out, scanFinding{e.Path, "large binary blob (" + humanBytes(e.Bytes) + ")"})
			continue
		}
		if e.Bytes >= oversizedBlobBytes {
			out = append(out, scanFinding{e.Path, "oversized file (" + humanBytes(e.Bytes) + ")"})
			continue
		}
	}
	return out
}

// vendoredSegment reports the first path segment that names a vendored tree.
func vendoredSegment(path string) (string, bool) {
	for _, seg := range strings.Split(path, "/") {
		if vendoredDirs[seg] {
			return seg, true
		}
	}
	return "", false
}

// secretLikePath reports whether a path is credential-shaped by basename or
// extension. `.env.example`/`.env.sample` are explicitly allowed (templates).
func secretLikePath(path string) (string, bool) {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	if secretBasenames[base] {
		return "credential-shaped file (" + base + ")", true
	}
	if strings.HasPrefix(base, ".env.") &&
		!strings.HasSuffix(base, ".example") && !strings.HasSuffix(base, ".sample") {
		return "environment file (" + base + ")", true
	}
	for _, suf := range secretSuffixes {
		if strings.HasSuffix(base, suf) {
			return "key material (" + base + ")", true
		}
	}
	return "", false
}

// humanBytes renders a size as a compact MiB/KiB string for issue text.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
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

// salvageReport is everything the issue text needs about one salvage.
type salvageReport struct {
	Repo     targetRepo
	Mode     string
	Branch   string
	Reason   reapReason
	Findings []scanFinding
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
}

// salvageIssueTitle is stable per repo+branch so duplicate detection works.
func salvageIssueTitle(r salvageReport) string {
	return fmt.Sprintf("%s %s: unmerged container work on %s",
		salvageIssueTitlePrefix, r.Repo.Name, r.Branch)
}

// salvageIssueBody renders the operator-facing issue: what happened, why it did
// not auto-land, how to recover the branch, and the junk-scan findings.
func salvageIssueBody(r salvageReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "An ephemeral `ward container` (%s mode) finished but its work was **not merged to `main`**, so the reaper preserved it on a branch before the container was torn down.\n\n", r.Mode)
	fmt.Fprintf(&b, "- **Repo:** `%s`\n", r.Repo.slug())
	fmt.Fprintf(&b, "- **Salvage branch:** `%s`\n", r.Branch)
	fmt.Fprintf(&b, "- **Reason:** %s\n", r.Reason)
	if r.TokenAge != "" {
		fmt.Fprintf(&b, "- **Container uptime at reap:** %s (age of the baked Forgejo PAT snapshot; a long-lived container is likelier to carry a rotated token)\n", r.TokenAge)
	}
	b.WriteString("\n")

	if r.AuthCause {
		b.WriteString("## Likely cause: dead/rotated PAT, not a conflict\n\n")
		b.WriteString("The push was rejected on **credentials**, not content. The Forgejo PAT baked into this container at `up` time was most likely rotated or revoked while it ran, so the final push to `main` (and any salvage-branch push) failed on auth. This is **not** a merge conflict - the work on the salvage branch should rebase and land cleanly once pushed with a live token. Don't rotate the PAT while containers are in flight; see docs/container-reap.md.\n\n")
	}

	b.WriteString("## Recover\n\n```bash\n")
	fmt.Fprintf(&b, "git fetch %s %s\n", r.Repo.cloneURL(r.Base), r.Branch)
	fmt.Fprintf(&b, "git checkout -b %s FETCH_HEAD\n", r.Branch)
	b.WriteString("```\n\n")

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

// sortedFindings returns findings ordered by path for deterministic rendering.
func sortedFindings(in []scanFinding) []scanFinding {
	out := append([]scanFinding(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
