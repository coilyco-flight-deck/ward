package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/valuesource"
	"github.com/urfave/cli/v3"
)

// wardkdl_exec.go auto-mounts the exec-dialect ward-kdl guardfiles into `ward`
// (ward#284) off the launch-selected source (ward#653). See docs/ward-kdl-in-ward.md.

// mountWardKdlExec grafts every exec guardfile in the launch-selected config
// source onto root at the path its `wrap ward-kdl <area>...` block names.
func mountWardKdlExec(root *cli.Command, r *Runner) error {
	src, err := selectConfigSource()
	if err != nil {
		return err
	}
	return mountWardKdlExecFrom(root, src, r)
}

// mountWardKdlExecFrom scans src.execDir and grafts each exec guardfile.
// Mixed bundles filter to the exec guardfile glob and skip spec siblings.
func mountWardKdlExecFrom(root *cli.Command, src configSource, r *Runner) error {
	r.configAuditVersion = src.auditVersion

	entries, err := fs.ReadDir(src.fsys, src.execDir)
	if err != nil {
		return fmt.Errorf("read exec guardfiles: %w", err)
	}
	// Sort so the mount order (and the --help listing) is deterministic.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if src.execMixedDialects {
			if ok, _ := path.Match(src.execGuardfileGlob, e.Name()); !ok {
				continue
			}
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		gfBytes, err := fs.ReadFile(src.fsys, path.Join(src.execDir, name))
		if err != nil {
			return fmt.Errorf("mount %s: read: %w", name, err)
		}
		if src.execMixedDialects && !isExecDialectGuardfile(gfBytes) {
			continue // spec-dialect sibling; it rides the specverb path (ops.go)
		}
		if err := mountOneWardKdlExec(root, gfBytes, r); err != nil {
			return fmt.Errorf("mount %s: %w", name, err)
		}
	}
	return nil
}

// mountOneWardKdlExec parses, builds, and grafts a single exec guardfile, a
// no-op when the leaf path is already taken (hand-written command wins).
func mountOneWardKdlExec(root *cli.Command, gfBytes []byte, r *Runner) error {
	gf, err := execverb.Parse(normalizeFirstInputSugar(gfBytes))
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if len(gf.Group) < 2 {
		return fmt.Errorf("wrap path %v needs a binary token plus a mount path", gf.Group)
	}
	group, err := execverb.Build(execverb.Config{
		Guardfile: gf,
		Wrap: func(s verb.Spec) cli.ActionFunc {
			return r.WrapVerb(s, r.Audit)
		},
		// Run nil shells out to the real local binary; env (e.g. ollama's
		// OLLAMA_HOST) resolves lazily at exec time, so mounting never hits SSM.
		Providers: map[string]valuesource.Provider{
			"ssm": r.ssmValueResolver,
		},
	})
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	// Drop the leading binary token (gf.Group[0] == "ward-kdl", which maps to
	// root); intermediate segments are created once and shared across siblings.
	parent := root
	for _, seg := range gf.Group[1 : len(gf.Group)-1] {
		parent = findOrCreateGroup(parent, seg)
	}
	if subCommandNamed(parent, group.Name) != nil {
		return nil // hand-written command owns this path; leave it untouched
	}
	parent.Commands = append(parent.Commands, group)
	return nil
}

// normalizeFirstInputSugar keeps the exec-dialect parser compatible with the
// guardfile shorthand `first input`, which ward accepts as `arg0`.
func normalizeFirstInputSugar(gfBytes []byte) []byte {
	return bytes.ReplaceAll(gfBytes, []byte("first input"), []byte("arg0"))
}

// findOrCreateGroup returns parent's subcommand named name, creating an empty
// group for an intermediate wrap-path segment when absent.
func findOrCreateGroup(parent *cli.Command, name string) *cli.Command {
	if c := subCommandNamed(parent, name); c != nil {
		return c
	}
	g := &cli.Command{
		Name:  name,
		Usage: fmt.Sprintf("%s verbs routed through the ward-kdl guardfile runtime", name),
	}
	parent.Commands = append(parent.Commands, g)
	return g
}
