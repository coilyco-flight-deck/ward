package main

import (
	"errors"
	"path/filepath"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/exitcode"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/gittree"
	"github.com/urfave/cli/v3"
)

// runExecGate runs the clean-tree gate for a `ward exec` repo verb,
// returning (state, overrideUsed, err). See docs/exec-verb.md.
func runExecGate(c *cli.Command, repoRoot, cfgPath, verbName string) (*gittree.State, bool, error) {
	override := false
	if root := c.Root(); root != nil {
		override = root.Bool("audit-override-dirty")
	}
	state, err := gittree.CheckClean(repoRoot)
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
		errors.New(state.FormatRefusal(verbName)),
		"commit/push the outstanding ward.yaml change and retry, or pass "+
			"--audit-override-dirty for a genuine emergency").
		WithReason("audit rows must bind to a committed ward.yaml so the verb argv can be reconstructed from git history")
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
