package main

import "github.com/urfave/cli/v3"

func commandNamed(cmds []*cli.Command, name string) *cli.Command {
	for _, cmd := range cmds {
		if cmd.Name == name {
			return cmd
		}
	}
	return nil
}

func commandNames(cmds []*cli.Command) []string {
	names := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		names = append(names, cmd.Name)
	}
	return names
}
