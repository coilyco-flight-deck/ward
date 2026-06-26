package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// gitGrepRemoteCommand builds `ward git grep-remote`, per-repo code search by
// ephemeral clone (no REST code-search on forgejo). See docs/git-verbs.md.
func gitGrepRemoteCommand() *cli.Command {
	return &cli.Command{
		Name:            "grep-remote",
		Usage:           "Shallow-clone <owner/repo> into a temp dir, git grep <pattern>, then discard the clone.",
		SkipFlagParsing: true,
		Description: `grep-remote runs per-repo code search without a server-side index. It
clones the named forgejo repo into an ephemeral temp dir (--depth 1,
through ward's clone gate), runs a read-only git grep over the tracked
files, and removes the clone on exit.

  ward git grep-remote <owner/repo> <pattern> [git-grep-flags...]

Scope is one repo's tracked contents at HEAD - there is no cross-repo or
server-side search. Everything after <owner/repo> is forwarded verbatim to
git grep (so -n, -i, -l, -e, regex, and pathspecs all work). The clone is
shallow and thrown away, so nothing persists on disk.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "git.grep-remote",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runGitGrepRemote(ctx, cmd.Args().Slice())
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runGitGrepRemote resolves the repo ref, shallow-clones it into a temp dir
// through the existing clone gate, runs git grep there, and removes the clone.
func (r *Runner) runGitGrepRemote(ctx context.Context, argv []string) error {
	if len(argv) < 2 {
		return fmt.Errorf("ward git grep-remote: usage: " +
			"`ward git grep-remote <owner/repo> <pattern> [git-grep-flags...]`")
	}
	repo, err := parseRepoRef(argv[0])
	if err != nil {
		return fmt.Errorf("ward git grep-remote: %w", err)
	}
	grepArgs := argv[1:]

	tmp, err := os.MkdirTemp("", "ward-grep-*")
	if err != nil {
		return fmt.Errorf("ward git grep-remote: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	// Clone through the same destination gate `ward git clone` uses; tmp sits
	// under an ephemeral root, so the gate admits any repo for a throwaway checkout.
	cloneArgv := []string{"--depth", "1", repo.cloneURL(forgejoBaseURL), tmp}
	if err := r.runGitClone(ctx, cloneArgv); err != nil {
		return fmt.Errorf("ward git grep-remote: clone %s: %w", repo.slug(), err)
	}

	grepArgv := append([]string{"grep"}, grepArgs...)
	if err := r.Runner.ExecIn(ctx, tmp, "git", grepArgv...); err != nil {
		// git grep exits 1 with no matching lines (like grep). That is a clean
		// "nothing found", not a failure - swallow it so the verb exits 0.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("ward git grep-remote: grep %s: %w", repo.slug(), err)
	}
	return nil
}
