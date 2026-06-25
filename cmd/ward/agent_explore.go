package main

import (
	"context"
	"fmt"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/verb"
	"github.com/urfave/cli/v3"
)

// agent_explore.go wires `ward agent explore`: the read-only sibling of `sandbox`.
// Same seedless bring-up, no push credential. See docs/agent-explore.md (ward#293).

// agentExploreCommand builds `ward agent explore`: a read-only scratch session that
// reads the repo but cannot push. Shares runScratchSession, readOnly=true (ward#293).
func agentExploreCommand() *cli.Command {
	return &cli.Command{
		Name: "explore",
		Usage: "Drop into a read-only interactive agent in a fresh ephemeral container (repo clone + operating " +
			"context) with no issue and no seed - reads the repo but cannot commit, push, or merge.",
		Flags: agentScratchFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			mode, err := agentDriver(c)
			if err != nil {
				return fmt.Errorf("ward agent explore: %w", err)
			}
			return r.WrapVerb(verb.Spec{
				Name:       "agent." + string(mode) + ".explore",
				SkipPolicy: true,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return r.runScratchSession(ctx, cmd, mode, true)
				},
			}, r.Audit)(ctx, c)
		},
	}
}
