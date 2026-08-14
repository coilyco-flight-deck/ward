package main

import (
	"context"
	"fmt"
	"os"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/verb"
	"github.com/urfave/cli/v3"
)

// agentReservationCacheCommand builds `ward agent reservations clear`, the
// first-class host-side cleanup path for the disposable reservation cache.
func agentReservationCacheCommand() *cli.Command {
	clearCmd := &cli.Command{
		Name:  "clear",
		Usage: "Clear the disposable reservation cache directory wholesale and recreate it.",
		Description: "clear removes ~/.ward/agent-reservations wholesale. The directory is cache-only, " +
			"so the safe emergency recovery path is to delete it as a unit instead of hunting per-issue " +
			"JSON or lock file names.",
		Action: func(ctx context.Context, c *cli.Command) error {
			r := newRunner()
			return r.WrapVerb(verb.Spec{
				Name:       "agent.reservations.clear",
				SkipPolicy: true, // cache-only host state; no repo tree to gate
				Action: func(context.Context, *cli.Command) error {
					if err := clearAgentReservationCacheDir(); err != nil {
						return fmt.Errorf("ward agent reservations clear: %w", err)
					}
					fmt.Fprintln(os.Stderr, "ward agent reservations clear: cleared the reservation cache directory on host ward")
					return nil
				},
			}, r.Audit)(ctx, c)
		},
	}
	return &cli.Command{
		Name:  "reservations",
		Usage: "Reservation cache maintenance for stale local state.",
		Description: "reservations groups the cache-only cleanup surface. The directory is disposable, " +
			"and `clear` is the supported whole-folder recovery path.",
		Commands: []*cli.Command{clearCmd},
	}
}
