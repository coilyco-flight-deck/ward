package main

import (
	"os"
	"path/filepath"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// dispatch.go is thin wiring over cli-guard's cli/dispatch package; ward only
// supplies the host seams. See docs/dispatch.md (the retired coily dispatch).

// allowedOwner anchors the dispatch infra dirs and the localRepoPath fallback;
// the trust check accepts the full primaryOrgs set. See docs/dispatch.md.
const allowedOwner = "coilysiren"

// localRepoPath resolves a repo name to the first existing checkout across the
// primary orgs, falling back to ~/projects/coilysiren/<repo>.
func (r *Runner) localRepoPath(repo string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, org := range r.primaryOrgs() {
		p := filepath.Join(home, "projects", org, repo)
		if st, statErr := os.Stat(p); statErr == nil && st.IsDir() {
			return p, nil
		}
	}
	return filepath.Join(home, "projects", allowedOwner, repo), nil
}

// dispatchWorktreeRoot is the parent dir for each detached dispatch's worktree:
// ~/projects/coilysiren/.dispatch-worktrees, outside any repo.
func dispatchWorktreeRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "projects", allowedOwner, ".dispatch-worktrees"), nil
}

// dispatchLogRoot is the parent dir for headless dispatch log files:
// ~/projects/coilysiren/.dispatch-logs, alongside the worktree root.
func dispatchLogRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "projects", allowedOwner, ".dispatch-logs"), nil
}

// dispatchRunner builds a Runner for the dispatch tree without newRunner's
// fatal audit preflight, so a lean verb like `ward version` never os.Exits.
func dispatchRunner() *Runner { return leanRunner() }

// dispatchCommand builds the dispatch umbrella verb from cli-guard, wiring
// ward's runner, audit pipeline, and workspace seams. See docs/dispatch.md.
func dispatchCommand() *cli.Command {
	r := dispatchRunner()
	d, err := dispatch.New(dispatch.Config{
		Runner: r.Runner,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			return r.WrapVerb(s, r.Audit)
		},
		AllowedOwner:      allowedOwner,
		AllowedOwners:     r.primaryOrgs(),
		BinaryName:        "ward",
		RepoPath:          r.localRepoPath,
		WorktreeRoot:      dispatchWorktreeRoot,
		LogRoot:           dispatchLogRoot,
		ForgejoBaseURL:    forgejoBaseURL,
		FetchForgejoIssue: r.fetchForgejoIssue,
	})
	if err != nil {
		panic("ward: dispatch wiring invalid: " + err.Error())
	}
	return d.Command()
}
