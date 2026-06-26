package main

import (
	"context"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/passthrough"
	"github.com/urfave/cli/v3"
)

// gitPassthroughVerbs is the audited git set fronted by `ward git <verb>`.
// commit/clone are dedicated verbs (git_commit.go, git_clone.go); see docs.
var gitPassthroughVerbs = []struct{ name, usage string }{
	{"status", "git status - show the working tree state."},
	{"log", "git log - show commit history."},
	{"diff", "git diff - show changes."},
	{"show", "git show - show a commit or object."},
	{"grep", "git grep - search tracked file contents (read-only)."},
	{"add", "git add - stage changes."},
	{"fetch", "git fetch - download objects and refs from a remote."},
	{"pull", "git pull - fetch and integrate."},
	{"push", "git push - update remote refs."},
	{"branch", "git branch - list or manage branches."},
	{"checkout", "git checkout - switch branches or restore files."},
	{"stash", "git stash - shelve working-tree changes."},
	{"restore", "git restore - restore working-tree files."},
	{"remote", "git remote - list remotes or inspect a remote's URL (get-url)."},
}

// gitCommand groups ward's audited git verbs: thin passthroughs plus the
// concurrency-safe commit and the destination-gated clone. See docs/git-verbs.md.
func gitCommand() *cli.Command {
	subs := []*cli.Command{gitCommitCommand(), gitCloneCommand(), gitGrepRemoteCommand()}
	for _, v := range gitPassthroughVerbs {
		subs = append(subs, gitPassthroughLeaf(v.name, v.usage))
	}
	return &cli.Command{
		Name:     "git",
		Usage:    "Audited git verbs for contributors.",
		Commands: subs,
		Description: `git fronts the contributor git surface behind cli-guard's audit +
argv-validation pipeline. Read/safe verbs are thin passthroughs; commit
is a dedicated concurrency-safe verb (see docs/git-verbs.md).`,
	}
}

// gitPassthroughLeaf builds one `ward git <verb>` passthrough, wiring the
// argv rewriter so `ward git <verb> ...` runs `git <verb> ...`.
func gitPassthroughLeaf(name, usage string) *cli.Command {
	return &cli.Command{
		Name:            name,
		Usage:           usage,
		SkipFlagParsing: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			pc := passthrough.Command("git", r.Runner, r.Audit,
				passthrough.WithVerbName("git."+name),
				passthrough.WithArgvRewriter(gitVerbRewriter(name)),
			)
			return pc.Action(ctx, c)
		},
	}
}

// gitVerbRewriter prepends the subcommand to the passthrough argv, hoisting
// a leading `-C <path>` ahead of the verb (git wants -C before the verb).
func gitVerbRewriter(verbName string) func([]string) []string {
	return func(argv []string) []string {
		if len(argv) >= 2 && argv[0] == "-C" {
			out := []string{"-C", argv[1], verbName}
			return append(out, argv[2:]...)
		}
		return append([]string{verbName}, argv...)
	}
}
