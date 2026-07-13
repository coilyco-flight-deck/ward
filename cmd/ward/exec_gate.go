package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/gittree"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/exitcode"
	"github.com/urfave/cli/v3"
)

// runExecGate runs the clean-tree gate for a `ward exec` repo verb,
// returning (state, overrideUsed, err). See docs/exec-verb.md.
func runExecGate(c *cli.Command, repoRoot, cfgPath, verbName string, readOnly bool) (*gittree.State, bool, error) {
	override := false
	if root := c.Root(); root != nil {
		override = root.Bool("audit-override-dirty")
	}
	var (
		state *gittree.State
		err   error
	)
	if readOnly {
		state, err = checkCleanReadOnly(repoRoot)
	} else {
		state, err = gittree.CheckClean(repoRoot)
	}
	if err != nil {
		return nil, false, exitcode.New(exitcode.Internal, "gittree_error", err,
			"ward could not evaluate the repo verb gate; run `git status` "+
				"in the repo to confirm it is in a sane state, then retry")
	}
	if state.Clean {
		return state, false, nil
	}
	if dirtIsOutsideWardConfig(state, repoRoot, cfgPath) {
		return state, false, nil
	}
	if override {
		return state, true, nil
	}
	return nil, false, exitcode.New(exitcode.PolicyDenied, "repo_verb_dirty",
		errors.New(formatExecGateRefusal(state, verbName)),
		"commit/push the outstanding ward.yaml change and retry, or pass "+
			"--audit-override-dirty for a genuine emergency").
		WithReason("audit rows must bind to a committed ward.yaml so the verb argv can be reconstructed from git history")
}

func formatExecGateRefusal(state *gittree.State, verbName string) string {
	switch {
	case state.Reason == "working tree is dirty":
		return state.FormatRefusal(verbName)
	case strings.HasPrefix(state.Reason, "branch ") && strings.HasSuffix(state.Reason, " has no upstream"):
		return refusalNoUpstream(state, verbName)
	case state.Reason == "HEAD is detached (no branch)":
		return refusalDetachedHead(state, verbName)
	case strings.Contains(state.Reason, " commits behind "):
		return refusalBehindUpstream(state, verbName)
	case strings.HasPrefix(state.Reason, "git fetch failed for "):
		return refusalFetchFailed(state, verbName)
	default:
		return state.FormatRefusal(verbName)
	}
}

func refusalNoUpstream(state *gittree.State, verbName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "refusing repo verb %q - %s\n", verbName, state.Reason)
	b.WriteString("\nRepo verbs require a branch with an upstream so the audit log can be reconstructed\n")
	b.WriteString("from git history. Recover with:\n\n")
	fmt.Fprintf(&b, "  git push -u origin %s\n", state.Branch)
	fmt.Fprintf(&b, "  %s %s   # retry\n", filepath.Base(os.Args[0]), verbName)
	return b.String()
}

func refusalDetachedHead(state *gittree.State, verbName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "refusing repo verb %q - %s\n", verbName, state.Reason)
	b.WriteString("\nRepo verbs require a checked-out branch so the audit log can be reconstructed\n")
	b.WriteString("from git history. Recover with:\n\n")
	b.WriteString("  git checkout <branch>\n")
	fmt.Fprintf(&b, "  %s %s   # retry\n", filepath.Base(os.Args[0]), verbName)
	return b.String()
}

func refusalBehindUpstream(state *gittree.State, verbName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "refusing repo verb %q - %s\n", verbName, state.Reason)
	b.WriteString("\nRepo verbs require a synced branch so the audit log can be reconstructed\n")
	b.WriteString("from git history. Recover with:\n\n")
	b.WriteString("  git pull --ff-only\n")
	fmt.Fprintf(&b, "  %s %s   # retry\n", filepath.Base(os.Args[0]), verbName)
	return b.String()
}

func refusalFetchFailed(state *gittree.State, verbName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "refusing repo verb %q - %s\n", verbName, state.Reason)
	b.WriteString("\nRepo verbs require a reachable upstream so the audit log can be reconstructed\n")
	b.WriteString("from git history. Recover with:\n\n")
	fmt.Fprintf(&b, "  git fetch %s\n", strings.TrimPrefix(state.Reason, "git fetch failed for "))
	fmt.Fprintf(&b, "  %s %s   # retry\n", filepath.Base(os.Args[0]), verbName)
	return b.String()
}

// checkCleanReadOnly mirrors the clean-tree gate without the upstream fetch
// step so read-only director surfaces can still run the local self-check.
func checkCleanReadOnly(repoRoot string) (*gittree.State, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("gittree: git binary not found on $PATH: %w", err)
	}
	if out, gerr := runGitReadOnly(repoRoot, "rev-parse", "--is-inside-work-tree"); gerr != nil || strings.TrimSpace(out) != "true" {
		return nil, fmt.Errorf("%w: %s", gittree.ErrNotGitRepo, repoRoot)
	}
	status, err := runGitReadOnly(repoRoot, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return nil, fmt.Errorf("gittree: git status: %w", err)
	}
	branch, err := runGitReadOnly(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("gittree: git rev-parse HEAD: %w", err)
	}
	st := &gittree.State{
		Status:     truncate(status, gittree.MaxStatusBytes),
		DirtyPaths: parseDirtyPaths(status),
		Branch:     strings.TrimSpace(branch),
	}
	if status != "" {
		st.Reason = "working tree is dirty"
		st.Recovery = recoveryDirtyReadOnly(st.Status)
		return st, nil
	}
	if st.Branch == "HEAD" {
		st.Reason = "HEAD is detached (no branch)"
		st.Recovery = "  git checkout <branch>\n"
		return st, nil
	}
	st.Clean = true
	return st, nil
}

func runGitReadOnly(repoRoot string, args ...string) (string, error) {
	full := append([]string{"-C", repoRoot}, args...)
	cmd := exec.Command("git", full...) // #nosec G204 -- args are caller-controlled, not user-shaped
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// dirtIsOutsideWardConfig reports whether a dirty-tree refusal leaves the
// declaring ward.yaml committed. Mirrors coily's dirtIsOutsideCoilyConfig.
func dirtIsOutsideWardConfig(state *gittree.State, repoRoot, cfgPath string) bool {
	if state.Reason != "working tree is dirty" {
		return false
	}
	rel, err := filepath.Rel(repoRoot, cfgPath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, p := range state.DirtyPaths {
		if filepath.ToSlash(p) == rel {
			return false
		}
	}
	return true
}

func recoveryDirtyReadOnly(status string) string {
	hasTracked := false
	hasUntracked := false
	for _, line := range strings.Split(status, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			hasUntracked = true
		} else {
			hasTracked = true
		}
	}
	var b strings.Builder
	b.WriteString("  git status              # see what's outstanding\n")
	if hasTracked {
		b.WriteString("  git add ... && git commit\n")
	}
	if hasUntracked {
		b.WriteString("  git add <untracked> && git commit   # or add to .gitignore\n")
	}
	b.WriteString("  git push\n")
	return b.String()
}

// parseDirtyPaths extracts dirty working-tree paths from porcelain v1 output.
func parseDirtyPaths(porcelain string) []string {
	if porcelain == "" {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) < 4 {
			continue
		}
		rest := line[3:]
		if arrow := strings.Index(rest, " -> "); arrow >= 0 {
			paths = append(paths, unquoteStatusPath(rest[:arrow]), unquoteStatusPath(rest[arrow+4:]))
			continue
		}
		paths = append(paths, unquoteStatusPath(rest))
	}
	return paths
}

func unquoteStatusPath(p string) string {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		return p[1 : len(p)-1]
	}
	return p
}
