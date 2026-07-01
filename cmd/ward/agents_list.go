package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/urfave/cli/v3"
)

// agents_list.go is `ward agents list [--json]`: the stable read surface dumping
// the embedded fleet roster from fleetconfig.Fleet. See docs/agents-list.md.

// agentsRosterJSON is the stable JSON shape `ward agents list --json` emits.
// Keys are always present so a consumer sees one deterministic schema.
type agentsRosterJSON struct {
	SchemaVersion int                `json:"schema_version"`
	Defaults      agentsDefaultsJSON `json:"defaults"`
	Agents        []agentsAgentJSON  `json:"agents"`
}

// agentsDefaultsJSON mirrors fleetconfig.Defaults.
type agentsDefaultsJSON struct {
	Agent       string                `json:"agent"`
	Attribution agentsAttributionJSON `json:"attribution"`
}

// agentsAttributionJSON mirrors fleetconfig.Attribution.
type agentsAttributionJSON struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// agentsAgentJSON mirrors fleetconfig.Agent: one agent's descriptive manifest
// fields. context_level carries the -1 unset sentinel verbatim.
type agentsAgentJSON struct {
	Name            string         `json:"name"`
	Binary          string         `json:"binary"`
	ContextLevel    int            `json:"context_level"`
	Stream          string         `json:"stream"`
	Auth            string         `json:"auth"`
	Model           string         `json:"model"`
	Endpoint        string         `json:"endpoint"`
	Provider        string         `json:"provider"`
	ReasoningEffort string         `json:"reasoning_effort"`
	Verbosity       string         `json:"verbosity"`
	Argv            agentsArgvJSON `json:"argv"`
}

// agentsArgvJSON mirrors fleetconfig.Argv: each mode's full token list, or null
// when the mode is not declared.
type agentsArgvJSON struct {
	Preflight   []string `json:"preflight"`
	Headless    []string `json:"headless"`
	Interactive []string `json:"interactive"`
}

// fleetToRosterJSON projects a parsed fleet onto the stable JSON shape. Pure over
// its input so the schema is testable without touching the embed or the CLI.
func fleetToRosterJSON(f fleetconfig.Fleet) agentsRosterJSON {
	out := agentsRosterJSON{
		SchemaVersion: f.SchemaVersion,
		Defaults: agentsDefaultsJSON{
			Agent: f.Defaults.Agent,
			Attribution: agentsAttributionJSON{
				Name:  f.Defaults.Attribution.Name,
				Email: f.Defaults.Attribution.Email,
			},
		},
		Agents: make([]agentsAgentJSON, 0, len(f.Agents)),
	}
	for _, a := range f.Agents {
		out.Agents = append(out.Agents, agentsAgentJSON{
			Name:            a.Name,
			Binary:          a.Binary,
			ContextLevel:    a.ContextLevel,
			Stream:          a.Stream,
			Auth:            a.Auth,
			Model:           a.Model,
			Endpoint:        a.Endpoint,
			Provider:        a.Provider,
			ReasoningEffort: a.ReasoningEffort,
			Verbosity:       a.Verbosity,
			Argv: agentsArgvJSON{
				Preflight:   a.Argv.Preflight,
				Headless:    a.Argv.Headless,
				Interactive: a.Argv.Interactive,
			},
		})
	}
	return out
}

// agentsRosterTable renders the human default: one block per agent with its
// binary, context-level floor, and model.
func agentsRosterTable(f fleetconfig.Fleet) string {
	var b []byte
	b = append(b, fmt.Sprintf("ward fleet roster (dialect %d, default agent %q, %d agents)\n",
		f.SchemaVersion, f.Defaults.Agent, len(f.Agents))...)
	for _, a := range f.Agents {
		model := a.Model
		if model == "" {
			model = "-"
		}
		b = append(b, fmt.Sprintf("\n  %s\n    binary:        %s\n    context-level: %d\n    model:         %s\n",
			a.Name, a.Binary, a.ContextLevel, model)...)
	}
	return string(b)
}

// agentsCommand is the hand-written `ward agents` group. The exec-guardfile
// launchers auto-mount beside `list` here (docs/ward-kdl-in-ward.md).
func agentsCommand() *cli.Command {
	return &cli.Command{
		Name:  "agents",
		Usage: "the agent fleet: `list` the roster, or launch a harness (`ward agents claude`, ...).",
		Commands: []*cli.Command{
			agentsListCommand(),
		},
	}
}

// agentsListCommand builds `ward agents list [--json]`: a read-only dump of the
// embedded fleet roster (human table, or --json for the surface aos consumes).
func agentsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "Print the embedded fleet roster (agents + manifest fields). --json emits the stable read surface aos consumes.",
		Description: `list dumps the fleet roster straight from the embedded fleetconfig.Fleet - the
same parse cmd/ward/fleet.go embeds - so the roster the binary launches and the
roster it reports can never drift. The default is a human table; --json emits a
stable, deterministic JSON schema (schema_version, defaults, agents[]) that aos's
scripts/agent-compat.py consumes as its read surface (ward#417).`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "emit the stable JSON roster instead of the human table"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			fleet, err := loadFleetConfig()
			if err != nil {
				return fmt.Errorf("ward agents list: load fleet config: %w", err)
			}
			w := c.Root().Writer
			if w == nil {
				w = os.Stdout
			}
			if c.Bool("json") {
				buf, err := json.MarshalIndent(fleetToRosterJSON(fleet), "", "  ")
				if err != nil {
					return fmt.Errorf("ward agents list: marshal roster: %w", err)
				}
				_, err = io.WriteString(w, string(buf)+"\n")
				return err
			}
			_, err = io.WriteString(w, agentsRosterTable(fleet))
			return err
		},
	}
}
