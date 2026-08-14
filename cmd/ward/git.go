package main

import (
	"context"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/passthrough"
	"github.com/urfave/cli/v3"
)

// gitPassthroughVerbs is the audited git set fronted by `ward git <verb>`; net
// marks the remote verbs that get pre-configured Forgejo auth (ward#507, git_auth.go).
var gitPassthroughVerbs = []struct {
	name, usage string
	net         bool
}{
	{name: "status", usage: "git status - show the working tree state."},
	{name: "log", usage: "git log - show commit history."},
	{name: "diff", usage: "git diff - show changes."},
	{name: "show", usage: "git show - show a commit or object."},
	{name: "grep", usage: "git grep - search tracked file contents (read-only)."},
	{name: "add", usage: "git add - stage changes."},
	{name: "fetch", usage: "git fetch - download objects and refs from a remote.", net: true},
	{name: "pull", usage: "git pull - fetch and integrate.", net: true},
	{name: "push", usage: "git push - update remote refs.", net: true},
	{name: "branch", usage: "git branch - list or manage branches."},
	{name: "checkout", usage: "git checkout - switch branches or restore files."},
	{name: "stash", usage: "git stash - shelve working-tree changes."},
	{name: "restore", usage: "git restore - restore working-tree files."},
	{name: "remote", usage: "git remote - list remotes or inspect a remote's URL (get-url)."},
}

// gitCommand groups ward's audited git verbs: thin passthroughs plus the
// concurrency-safe commit and the destination-gated clone. See docs/git-verbs.md.
func gitCommand() *cli.Command {
	subs := []*cli.Command{gitCommitCommand(), gitCloneCommand(), gitGrepRemoteCommand()}
	for _, v := range gitPassthroughVerbs {
		subs = append(subs, gitPassthroughLeaf(v.name, v.usage, v.net))
	}
	return &cli.Command{
		Name:     "git",
		Usage:    "Audited git verbs for contributors.",
		Commands: subs,
		Description: `git fronts the contributor git surface behind umbra's audit +
argv-validation pipeline. Read/safe verbs are thin passthroughs; commit
is a dedicated concurrency-safe verb (see docs/git-verbs.md).`,
	}
}

// gitPassthroughLeaf builds one `ward git <verb>` passthrough (argv rewriter
// prepends the verb); a net verb also gets Forgejo auth injected (ward#507).
func gitPassthroughLeaf(name, usage string, net bool) *cli.Command {
	return &cli.Command{
		Name:            name,
		Usage:           usage,
		SkipFlagParsing: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			opts := []passthrough.Option{
				passthrough.WithVerbName("git." + name),
				passthrough.WithArgvRewriter(gitVerbRewriter(name)),
			}
			if net {
				opts = append(opts, passthrough.WithEnvFunc(func() (map[string]string, error) {
					return r.gitForgejoAuthEnv(ctx)
				}))
			}
			pc := passthrough.Command("git", r.Runner, r.Audit, opts...)
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
