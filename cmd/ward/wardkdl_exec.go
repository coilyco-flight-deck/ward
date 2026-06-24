package main

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/execverb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/valuesource"
	"github.com/urfave/cli/v3"
)

// wardkdl_exec.go auto-mounts the exec-dialect ward-kdl guardfiles into `ward`,
// generalizing graftForgejoAdminExec (ward#284). See docs/ward-kdl-in-ward.md.

// execAssets embeds the exec guardfiles mirrored from cmd/ward-kdl by `make
// sync-exec-assets` (go:embed can't reach the sibling dir; the drift test guards it).

//go:embed execassets/*.guardfile.kdl
var execAssets embed.FS

// execAssetsDir is the embed sub-path holding the mirrored guardfiles.
const execAssetsDir = "execassets"

// mountWardKdlExec grafts every embedded exec guardfile onto root at the path
// its `wrap ward-kdl <area>...` block names. See docs/ward-kdl-in-ward.md.
func mountWardKdlExec(root *cli.Command, r *Runner) error {
	entries, err := fs.ReadDir(execAssets, execAssetsDir)
	if err != nil {
		return fmt.Errorf("read embedded exec guardfiles: %w", err)
	}
	// Sort so the mount order (and the --help listing) is deterministic.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := mountOneWardKdlExec(root, execAssetsDir+"/"+name, r); err != nil {
			return fmt.Errorf("mount %s: %w", name, err)
		}
	}
	return nil
}

// mountOneWardKdlExec parses, builds, and grafts a single exec guardfile, a
// no-op when the leaf path is already taken (hand-written command wins).
func mountOneWardKdlExec(root *cli.Command, embedPath string, r *Runner) error {
	gfBytes, err := execAssets.ReadFile(embedPath)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	gf, err := execverb.Parse(gfBytes)
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
