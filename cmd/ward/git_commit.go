package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/shell"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/verb"
	"github.com/urfave/cli/v3"
)

// gitCommitCommand builds the dedicated, concurrency-safe `ward git commit`.
// Ported from coily's git commit (coily#7). See docs/git-verbs.md.
func gitCommitCommand() *cli.Command {
	return &cli.Command{
		Name:            "commit",
		Usage:           "git commit - record named paths atomically (concurrency-safe).",
		SkipFlagParsing: true,
		Description: `commit records changes for explicitly named paths in a way that is
safe when multiple sessions share one working tree. It requires:

  ward git commit -m "msg" -- <path> [<path>...]

Paths after '--' are committed from the working tree, seeded from HEAD,
against a private index, so another session's staged files never leak in
and your message (from -m/-F, never the editor) never crosses with
theirs. A leading '-C <dir>' selects a repo other than cwd. See
docs/git-verbs.md.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "git.commit",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runGitCommit(ctx, cmd.Args().Slice())
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runGitCommit validates argv, then stages+commits the named paths against
// a private index and resyncs the shared index for those paths.
func (r *Runner) runGitCommit(ctx context.Context, argv []string) error {
	dir, rest := hoistDashC(argv)
	flags, paths, err := splitCommitArgs(rest)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("ward git commit: name the files to commit after '--', e.g. " +
			"`ward git commit -m \"msg\" -- path/to/file` - bare commit is refused so a " +
			"concurrent session's staged files cannot cross into this commit")
	}
	if !hasMessageSource(flags) {
		return fmt.Errorf("ward git commit: pass -m/-F (the editor is refused under a shared " +
			"working tree so messages cannot cross between sessions)")
	}
	if hasEditorFlag(flags) {
		return fmt.Errorf("ward git commit: -e/--edit is refused under a shared working tree; " +
			"supply the message with -m/-F")
	}

	idxPath, cleanup, err := newPrivateIndex()
	if err != nil {
		return err
	}
	defer cleanup()

	// Private-index runner: identical streams plus GIT_INDEX_FILE so this
	// commit never reads or writes the shared .git/index.
	idxRunner := &shell.Runner{
		Stdout:  r.Runner.Stdout,
		Stderr:  r.Runner.Stderr,
		Stdin:   r.Runner.Stdin,
		Resolve: r.Runner.Resolve,
		Env:     []string{"GIT_INDEX_FILE=" + idxPath},
	}

	if err := seedPrivateIndex(ctx, idxRunner, dir, paths); err != nil {
		return err
	}

	commitArgv := gitArgv(dir, "commit", flags, paths)
	if err := idxRunner.Exec(ctx, "git", commitArgv...); err != nil {
		return fmt.Errorf("ward git commit: %w", err)
	}

	// Best-effort: resync only the committed paths in the shared index to
	// the new HEAD so the next `git status` reads clean for them.
	resyncArgv := gitArgv(dir, "reset", []string{"-q", "HEAD"}, paths)
	if err := r.Runner.Exec(ctx, "git", resyncArgv...); err != nil {
		fmt.Fprintf(os.Stderr, "ward git commit: committed, but resyncing the shared index for "+
			"the committed paths failed (%v); `git status` may show them until the next index "+
			"update\n", err)
	}
	return nil
}

// seedPrivateIndex primes the private index from HEAD then stages the
// worktree state of exactly the named paths (so new files commit too).
func seedPrivateIndex(ctx context.Context, idxRunner *shell.Runner, dir string, paths []string) error {
	// read-tree HEAD through a stderr-discarding copy: the first commit has
	// no HEAD, where read-tree fails and an empty index is already correct.
	readTreeArgv := []string{}
	if dir != "" {
		readTreeArgv = append(readTreeArgv, "-C", dir)
	}
	readTreeArgv = append(readTreeArgv, "read-tree", "HEAD")
	quiet := *idxRunner
	quiet.Stderr = io.Discard
	_ = quiet.Exec(ctx, "git", readTreeArgv...)

	addArgv := gitArgv(dir, "add", []string{"-A"}, paths)
	if err := idxRunner.Exec(ctx, "git", addArgv...); err != nil {
		return fmt.Errorf("ward git commit: staging the named paths failed: %w", err)
	}
	return nil
}

// hoistDashC peels a leading `-C <dir>` off argv (the convention for
// operating on a repo other than cwd) and returns it plus the remainder.
func hoistDashC(argv []string) (dir string, rest []string) {
	if len(argv) >= 2 && argv[0] == "-C" {
		return argv[1], argv[2:]
	}
	return "", argv
}

// splitCommitArgs partitions the post-(-C) argv at the first `--` into the
// commit flags and the pathspecs. A `--` is required.
func splitCommitArgs(argv []string) (flags, paths []string, err error) {
	for i, a := range argv {
		if a == "--" {
			return argv[:i], argv[i+1:], nil
		}
	}
	return nil, nil, fmt.Errorf("ward git commit: missing '--' separator; use " +
		"`ward git commit -m \"msg\" -- <path>...` so the files to commit are explicit")
}

// hasMessageSource reports whether the flags carry a non-interactive commit
// message (-m/--message or -F/--file in any spelling).
func hasMessageSource(flags []string) bool {
	for _, f := range flags {
		switch {
		case f == "-m" || f == "-F":
			return true
		case strings.HasPrefix(f, "-m") && len(f) > 2:
			return true
		case strings.HasPrefix(f, "-F") && len(f) > 2:
			return true
		case f == "--message" || strings.HasPrefix(f, "--message="):
			return true
		case f == "--file" || strings.HasPrefix(f, "--file="):
			return true
		}
	}
	return false
}

// hasEditorFlag reports whether the flags explicitly request the editor,
// refused even when a message source is also present.
func hasEditorFlag(flags []string) bool {
	for _, f := range flags {
		if f == "-e" || f == "--edit" {
			return true
		}
	}
	return false
}

// gitArgv assembles `[-C <dir>] <verb> <flags...> -- <paths...>`, with -C
// leading so it lands before the subcommand as git requires.
func gitArgv(dir, verbName string, flags, paths []string) []string {
	out := make([]string, 0, len(flags)+len(paths)+4)
	if dir != "" {
		out = append(out, "-C", dir)
	}
	out = append(out, verbName)
	out = append(out, flags...)
	out = append(out, "--")
	out = append(out, paths...)
	return out
}

// newPrivateIndex reserves a unique throwaway GIT_INDEX_FILE path, removing
// the placeholder so git creates the index fresh; seedPrivateIndex primes it.
func newPrivateIndex() (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "ward-gitidx-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("ward git commit: create private index: %w", err)
	}
	path = f.Name()
	_ = f.Close()
	_ = os.Remove(path)
	return path, func() { _ = os.Remove(path) }, nil
}
