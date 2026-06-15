package main

import "github.com/urfave/cli/v3"

// pkgCommand groups ward's audited package-manager wrappers under `ward
// pkg <tool>`. Currently brew only - the ward-native `coily pkg brew`.
func pkgCommand() *cli.Command {
	return &cli.Command{
		Name:  "pkg",
		Usage: "Package-manager pass-throughs (audited).",
		Description: `pkg groups ward's audited package-manager wrappers. Each subcommand
emits an audit record to ~/.ward/audit/<repo>.jsonl, so 'ward pkg brew
bundle' runs 'brew bundle' under ward's audit + scope rules - the
ward-native equivalent of 'coily pkg brew'.`,
		Commands: []*cli.Command{
			pkgBrewCommand(),
		},
	}
}
