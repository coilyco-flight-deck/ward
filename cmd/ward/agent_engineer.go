package main

import (
	"context"
	"fmt"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_engineer.go wires `ward agent engineer`, the detached-only implement-a-ticket
// role (ward#347; the attach was dropped in ward#356). See docs/agent-engineer.md.

// agentEngineerFlags is the engineer flag set: the shared detached carry flags
// (+ --no-preflight) and freeform instructions. No --watch/--new-tab (ward#356).
func agentEngineerFlags() []cli.Flag {
	flags := agentSurfaceFlags()
	// Freeform mode files an issue first (was `task`): the positional carries the text,
	// these escape hatches handle a long body or a bare owner/repo + instructions.
	flags = append(flags,
		&cli.StringFlag{Name: "instructions", Aliases: []string{"i"}, Usage: "freeform mode: the task to file as the issue body (first line becomes the title)"},
		&cli.StringFlag{Name: "instructions-file", Usage: "freeform mode: read the instructions from a file instead of --instructions (escape hatch for long bodies)"},
	)
	return flags
}

// agentEngineerCommand builds `ward agent engineer`: a ref carries a ticket detached,
// freeform files one first then carries it (detached too). docs/agent-engineer.md.
func agentEngineerCommand() *cli.Command {
	return &cli.Command{
		Name: "engineer",
		Usage: "Implement a ticket end to end: a ref carries it detached, " +
			"freeform text files an issue first then carries it.",
		ArgsUsage: "<owner/repo#N | #N | forgejo-issue-url | '<freeform instructions>'>",
		Flags:     agentEngineerFlags(),
		Action:    agentEngineerAction(),
	}
}

// agentEngineerAction builds the audited engineer action; it is also the umbrella
// default so a bare ref routes to the engineer carry (ward#282, ward#347).
func agentEngineerAction() cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		r := newRunner()
		mode, err := agentDriver(c)
		if err != nil {
			return fmt.Errorf("ward agent engineer: %w", err)
		}
		return r.WrapVerb(verb.Spec{
			Name:       "agent." + string(mode) + ".engineer",
			SkipPolicy: true,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return r.runAgentEngineer(ctx, cmd, mode)
			},
		}, r.Audit)(ctx, c)
	}
}

// runAgentEngineer dispatches by argument type (ward#347): a parseable ref carries it
// detached; anything else files an issue then carries it (detached-only; ward#356).
func (r *Runner) runAgentEngineer(ctx context.Context, c *cli.Command, mode containerMode) error {
	arg := strings.TrimSpace(c.Args().First())
	if _, err := parseAgentIssueRef(arg); err != nil {
		// Not an issue ref: freeform instructions (or a bare owner/repo + --instructions).
		return r.runAgentTask(ctx, c, mode)
	}
	if forwarded, err := r.maybeForwardAgentDispatchToHostBroker(ctx, c, "engineer", mode); forwarded {
		return err
	}
	// A ref always carries detached, fire-and-forget (ward#356).
	return r.runAgentWork(ctx, c, mode, "engineer")
}
