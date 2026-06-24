package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// cloneAllowlist is the baked-in set of repos (owner/name, lowercased) that may
// be cloned into a persistent path. Never read from disk. See docs/git-clone.md.
var cloneAllowlist = map[string]bool{
	"coilyco-flight-deck/agentic-os":          true,
	"coilyco-flight-deck/agentic-os-hardware": true,
	"coilyco-flight-deck/cli-guard":           true,
	"coilyco-flight-deck/infrastructure":      true,
	"coilyco-flight-deck/ward":                true,
	"coilysiren/coilysiren":                   true,
	"coilyco-bridge/agentic-os-kai":           true,
	"coilyco-bridge/deploy":                   true,
	"coilyco-bridge/lore":                     true,
}

// cloneValueFlags are the `git clone` options that consume the next argv token,
// so the destination scanner skips it rather than treating it as a positional.
var cloneValueFlags = map[string]bool{
	"-o": true, "--origin": true,
	"-b": true, "--branch": true,
	"-u": true, "--upload-pack": true,
	"-c": true, "--config": true,
	"-j": true, "--jobs": true,
	"--reference": true, "--reference-if-able": true,
	"--separate-git-dir": true,
	"--depth":            true,
	"--server-option":    true,
	"--bundle-uri":       true,
	"--shallow-exclude":  true,
}

// gitCloneCommand builds `ward git clone`, the destination-gated clone (raw
// `git clone` is denied in the agent lockdown). See docs/git-clone.md.
func gitCloneCommand() *cli.Command {
	return &cli.Command{
		Name:            "clone",
		Usage:           "git clone - destination-gated clone (ephemeral root OR allowlisted repo).",
		SkipFlagParsing: true,
		Description: `clone wraps git clone behind a destination gate. A clone is allowed
only if EITHER its resolved destination is under an ephemeral root
(/tmp or $TMPDIR) - any repo, since the checkout is throwaway - OR the
repository (owner/name parsed from the URL) is on ward's hardcoded
allowlist, in which case a persistent destination is fine.

  ward git clone <url> [dir]

The effective destination is resolved cwd-aware (the explicit [dir], else
cwd/<basename-of-url>, since bare git clone lands in cwd) to an absolute
path before the gate runs - which is why this is a Go verb, not a
guardfile glob. A leading '-C <dir>' selects the base directory. The
allowlist is baked into the binary; it is never read from disk. See
docs/git-clone.md.`,
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "git.clone",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runGitClone(ctx, cmd.Args().Slice())
				},
			}, r.Audit)(ctx, c)
		},
	}
}

// runGitClone resolves the effective destination, runs the gate, then execs the
// real `git clone` with the original argv. See docs/git-clone.md.
func (r *Runner) runGitClone(ctx context.Context, argv []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("ward git clone: resolve cwd: %w", err)
	}

	dirC, rest := hoistDashC(argv)
	base := cwd
	if dirC != "" {
		base = absFrom(cwd, dirC)
	}

	repoURL, cloneDir, err := splitCloneArgs(rest)
	if err != nil {
		return err
	}

	dest := destFromCloneArgs(base, repoURL, cloneDir)
	if err := cloneGate(repoURL, dest, os.Getenv); err != nil {
		return err
	}

	cloneArgv := make([]string, 0, len(rest)+3)
	if dirC != "" {
		cloneArgv = append(cloneArgv, "-C", dirC)
	}
	cloneArgv = append(cloneArgv, "clone")
	cloneArgv = append(cloneArgv, rest...)
	if err := r.Runner.Exec(ctx, "git", cloneArgv...); err != nil {
		return fmt.Errorf("ward git clone: %w", err)
	}
	return nil
}

// splitCloneArgs scans the post-(-C) argv for the two clone positionals (the
// URL and optional dir), skipping flags and value-flag values. `--` ends flags.
func splitCloneArgs(argv []string) (repoURL, cloneDir string, err error) {
	var positionals []string
	onlyPositional := false
	for i := 0; i < len(argv); i++ {
		tok := argv[i]
		switch {
		case onlyPositional:
			positionals = append(positionals, tok)
		case tok == "--":
			onlyPositional = true
		case strings.HasPrefix(tok, "-") && tok != "-":
			// A separate-value flag eats the next token; attached and bool do not.
			if cloneValueFlags[tok] && i+1 < len(argv) {
				i++
			}
		default:
			positionals = append(positionals, tok)
		}
	}
	if len(positionals) == 0 {
		return "", "", fmt.Errorf("ward git clone: name the repository to clone, e.g. " +
			"`ward git clone <url> [dir]`")
	}
	if len(positionals) > 2 {
		return "", "", fmt.Errorf("ward git clone: too many positional arguments %v; "+
			"expected `<url> [dir]`", positionals)
	}
	if len(positionals) == 2 {
		return positionals[0], positionals[1], nil
	}
	return positionals[0], "", nil
}

// destFromCloneArgs resolves the effective destination to an absolute path: the
// explicit dir (relative to base), else base/<humanish name of the URL>.
func destFromCloneArgs(base, repoURL, cloneDir string) string {
	if cloneDir != "" {
		return absFrom(base, cloneDir)
	}
	return absFrom(base, humanishName(repoURL))
}

// cloneGate refuses the clone unless the destination is under an ephemeral root
// OR the repo is allowlisted - two independent checks. See docs/git-clone.md.
func cloneGate(repoURL, destAbs string, getenv func(string) string) error {
	if destUnderEphemeral(destAbs, getenv) {
		return nil
	}
	owner, name, ok := repoFromURL(repoURL)
	if ok && cloneAllowlist[owner+"/"+name] {
		return nil
	}
	return fmt.Errorf("ward git clone: refused - %q is not under an ephemeral root "+
		"(/tmp or $TMPDIR) and %q is not on ward's hardcoded clone allowlist. Clone "+
		"off-allowlist repos into /tmp, or add the repo to the allowlist in "+
		"cmd/ward/git_clone.go if it legitimately belongs on disk (ward#285)",
		destAbs, repoURL)
}

// destUnderEphemeral reports whether destAbs resolves under any ephemeral root,
// comparing symlink-canonicalized paths so /tmp and $TMPDIR aliases both match.
func destUnderEphemeral(destAbs string, getenv func(string) string) bool {
	dest := canonicalize(destAbs)
	for _, root := range ephemeralRoots(getenv) {
		if pathWithin(dest, canonicalize(root)) {
			return true
		}
	}
	return false
}

// ephemeralRoots gathers the candidate ephemeral roots: /tmp, the platform temp
// dir (os.TempDir honors TMPDIR, falls back to /tmp), and an explicit $TMPDIR.
func ephemeralRoots(getenv func(string) string) []string {
	roots := []string{"/tmp", os.TempDir()}
	if t := getenv("TMPDIR"); t != "" {
		roots = append(roots, t)
	}
	return roots
}

// pathWithin reports whether path equals root or sits beneath it, on path
// boundaries so /tmpfoo is not treated as under /tmp.
func pathWithin(path, root string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// canonicalize resolves symlinks on the longest existing ancestor of path (the
// destination does not exist yet) and rejoins the missing tail.
func canonicalize(path string) string {
	path = filepath.Clean(path)
	rest := ""
	cur := path
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path // nothing on this path exists; use it as-is
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// absFrom resolves p to an absolute path, treating a relative p as relative to
// base (which may differ from cwd under a leading -C).
func absFrom(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(base, p))
}

// repoFromURL parses owner/name (lowercased) from a clone URL: scp-like, scheme
// URLs, or bare host paths. ok is false for local paths (never allowlistable).
func repoFromURL(raw string) (owner, name string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	var path string
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", false
		}
		if u.Scheme == "file" {
			return "", "", false
		}
		path = u.Path
	case isScpLike(raw):
		path = raw[strings.IndexByte(raw, ':')+1:]
	default:
		return "", "", false // bare local path: no owner/name
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner = strings.ToLower(parts[len(parts)-2])
	name = strings.ToLower(parts[len(parts)-1])
	if owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}

// isScpLike reports whether raw is git's scp-like syntax (host:path, no scheme),
// distinguishing it from an absolute or relative local path.
func isScpLike(raw string) bool {
	colon := strings.IndexByte(raw, ':')
	if colon < 1 {
		return false
	}
	slash := strings.IndexByte(raw, '/')
	return slash == -1 || colon < slash // colon must precede any slash
}

// humanishName derives git's default directory from a URL: the final path
// component with a trailing slash and `.git`/`.bundle` suffix stripped.
func humanishName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	raw = strings.TrimSuffix(raw, ".bundle")
	if i := strings.LastIndexAny(raw, "/:"); i >= 0 {
		raw = raw[i+1:]
	}
	return raw
}
