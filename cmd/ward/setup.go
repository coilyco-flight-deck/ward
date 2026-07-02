package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"
)

// setupCommand is ward's guided onboarding verb: scaffold .ward/ward.yaml from
// the Makefile, drop a commented security: block, then run doctor. See docs/setup.md.
func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Scaffold .ward/ward.yaml from the Makefile, then run doctor.",
		Description: "Walks up from the target dir to the repo root (the dir holding a " +
			"Makefile), turns every Makefile target carrying a `## <description>` help " +
			"comment into a `run: make <name>` verb, writes .ward/ward.yaml with a " +
			"commented security: scaffold, then runs `ward doctor` against it. Refuses to " +
			"clobber an existing config without --force. Prune the verbs you do not want " +
			"and uncomment the security: block to tailor it. Authors no guardfiles.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "dir",
				Usage: "Directory to start the Makefile/repo-root walk from (default: cwd).",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Overwrite an existing .ward/ward.yaml instead of refusing.",
			},
			&cli.BoolFlag{
				Name:  "skip-doctor",
				Usage: "Write the config but do not run doctor afterward.",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runSetup(os.Stdout, setupOptions{
				dir:        cmd.String("dir"),
				force:      cmd.Bool("force"),
				skipDoctor: cmd.Bool("skip-doctor"),
			})
		},
	}
}

// setupOptions tunes ward setup. The zero value scaffolds from cwd, refuses to
// overwrite, and runs doctor.
type setupOptions struct {
	dir        string
	force      bool
	skipDoctor bool
}

// runSetup locates the repo root, parses the Makefile, writes .ward/ward.yaml,
// then hands off to doctor. Any step's error is returned for a non-zero exit.
func runSetup(out io.Writer, opts setupOptions) error {
	start := opts.dir
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		start = cwd
	}

	root, makefilePath, err := findMakefileRoot(start)
	if err != nil {
		return err
	}

	targets, skipped, err := parseMakefileTargets(makefilePath)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("ward setup: no Makefile targets with a `## <description>` help comment in %s\n%s",
			makefilePath, allowlistContractHint)
	}

	yamlPath := filepath.Join(root, ".ward", "ward.yaml")
	if err := writeScaffold(yamlPath, renderWardYAML(targets), opts.force); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "ward setup: wrote %s (%d verb(s) from %s)\n", yamlPath, len(targets), makefilePath)
	for _, s := range skipped {
		_, _ = fmt.Fprintf(out, "ward setup: skipped Makefile target %q (not a valid verb name: a-z, 0-9, -)\n", s)
	}
	_, _ = fmt.Fprintln(out, "ward setup: prune verbs you do not want, and uncomment the security: block to tailor it.")

	if opts.skipDoctor {
		return nil
	}
	_, _ = fmt.Fprintln(out, "ward setup: running doctor against the generated config...")
	// A fresh scaffold's security: block is commented (inert), so allow the
	// ward#450 no-security: state here as a NOTE. Standalone doctor still fails.
	return runDoctorAt(out, yamlPath, doctorOptions{allowMissingSecurity: true})
}

// writeScaffold writes content to yamlPath, refusing to clobber an existing
// file unless force is set, and creating the parent .ward dir as needed.
func writeScaffold(yamlPath, content string, force bool) error {
	if _, statErr := os.Stat(yamlPath); statErr == nil && !force {
		return fmt.Errorf("ward setup: %s already exists; re-run with --force to overwrite", yamlPath)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("ward setup: stat %s: %w", yamlPath, statErr)
	}
	if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
		return fmt.Errorf("ward setup: mkdir %s: %w", filepath.Dir(yamlPath), err)
	}
	if err := os.WriteFile(yamlPath, []byte(content), 0o644); err != nil { // #nosec G306 -- config is meant to be world-readable
		return fmt.Errorf("ward setup: write %s: %w", yamlPath, err)
	}
	return nil
}

// findMakefileRoot walks up from start to the dir holding a Makefile (the repo
// root for scaffold purposes), returning that dir and the Makefile path.
func findMakefileRoot(start string) (root, makefilePath string, err error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", "", fmt.Errorf("ward setup: abs %s: %w", start, err)
	}
	for {
		candidate := filepath.Join(dir, "Makefile")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return dir, candidate, nil
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return "", "", fmt.Errorf("ward setup: stat %s: %w", candidate, statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("ward setup: no Makefile found walking up from %s", start)
		}
		dir = parent
	}
}

// setupTarget is one Makefile target destined for a ward.yaml verb: the target
// name and its trimmed `## <description>` help text.
type setupTarget struct {
	name        string
	description string
}

// setupTargetHelp matches `target: deps  ## description` help lines, kept in
// lockstep with cli-guard's allowlist.makeTargetHelp so doctor accepts the output.
var setupTargetHelp = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:[^=]*?##\s*(.*)$`)

// parseMakefileTargets returns the documented targets in source order, plus the
// names skipped as non-verbs. A duplicate target keeps its first-seen description.
func parseMakefileTargets(path string) (targets []setupTarget, skipped []string, err error) {
	f, err := os.Open(filepath.Clean(path)) // #nosec G304 -- caller-resolved repo-local Makefile path
	if err != nil {
		return nil, nil, fmt.Errorf("ward setup: read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m := setupTargetHelp.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !validVerbName(name) {
			skipped = append(skipped, name)
			continue
		}
		targets = append(targets, setupTarget{name: name, description: strings.TrimSpace(m[2])})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("ward setup: scan %s: %w", path, err)
	}
	return targets, skipped, nil
}

// validVerbName mirrors repocfg.validateName: a ward.yaml command key is
// lowercase letters, digits, and interior dashes only.
func validVerbName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// setupSecurityScaffold is the commented security: block ward setup appends. It
// is inert until uncommented, so a fresh scaffold parses to "no security declared".
const setupSecurityScaffold = `
# security: (optional) declares host-tool policy that ` + "`ward doctor`" + ` and the
# PreToolUse hook enforce. It is commented out so the scaffold is inert; uncomment
# and tailor it to your fleet. See docs/doctor.md and the cli-guard repocfg schema.
#
# security:
#   protected_binaries:
#     - name: gcloud
#       allowed_wrappers: [kap]
#       expected_real_paths: [/opt/homebrew/bin/gcloud]
#       credential_env: [GOOGLE_APPLICATION_CREDENTIALS]
#   sudo:
#     forbid_passwordless: true
#   hooks:
#     deny_bare_binaries: [gcloud]
#     route_hints:
#       gcloud: Use kap for cloud operations.
`

// renderWardYAML renders the scaffold: header, one verb per documented target
// (description verbatim so the doctor contract holds), then the security: block.
func renderWardYAML(targets []setupTarget) string {
	var b strings.Builder
	b.WriteString("# .ward/ward.yaml - scaffolded by `ward setup` from the Makefile.\n")
	b.WriteString("# Each verb mirrors a Makefile target's `## <description>` help comment, so\n")
	b.WriteString("# `ward exec <verb>` runs `make <verb>`. Prune what you do not want to expose;\n")
	b.WriteString("# the ward.yaml <-> Makefile contract is enforced by `ward doctor` (docs/doctor.md).\n\n")
	b.WriteString("commands:\n")
	for _, t := range targets {
		fmt.Fprintf(&b, "  %s:\n", t.name)
		fmt.Fprintf(&b, "    run: make %s\n", t.name)
		fmt.Fprintf(&b, "    description: %s\n", yamlScalar(t.description))
	}
	b.WriteString(setupSecurityScaffold)
	return b.String()
}

// yamlScalar renders s as a YAML scalar, double-quoting (minimal escaping) only
// when a plain scalar would be misparsed. Quoting round-trips to the same string.
func yamlScalar(s string) string {
	if isPlainYAMLScalar(s) {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// isPlainYAMLScalar reports whether s is safe unquoted: no surrounding space, no
// comment/quote/control char, no `: ` or trailing colon, no leading indicator.
func isPlainYAMLScalar(s string) bool {
	if s == "" || strings.TrimSpace(s) != s {
		return false
	}
	if strings.ContainsAny(s, "\"#\n\t") {
		return false
	}
	if strings.Contains(s, ": ") || strings.HasSuffix(s, ":") {
		return false
	}
	switch s[0] {
	case '!', '&', '*', '?', '|', '>', '%', '@', '`', '{', '}', '[', ']', ',', ':', '#', '-', '\'', ' ':
		return false
	}
	return true
}
