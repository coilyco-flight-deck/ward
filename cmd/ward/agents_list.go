package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"
)

// agents_list.go is `ward agents list [--json]`: the stable read surface for
// Ward's fixed harness adapters. See docs/agents-list.md.

// agentsRosterJSON is the stable JSON shape `ward agents list --json` emits.
// Keys are always present so a consumer sees one deterministic schema.
type agentsRosterJSON struct {
	SchemaVersion int                `json:"schema_version"`
	Defaults      agentsDefaultsJSON `json:"defaults"`
	Agents        []agentsAgentJSON  `json:"agents"`
}

type agentsDefaultsJSON struct {
	Agent       string                `json:"agent"`
	Attribution agentsAttributionJSON `json:"attribution"`
}

type agentsAttributionJSON struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

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

type agentsArgvJSON struct {
	Preflight   []string `json:"preflight"`
	Headless    []string `json:"headless"`
	Interactive []string `json:"interactive"`
}

// launchConfigToRosterJSON projects typed config onto the stable JSON shape.
// It is pure so tests need no CLI.
func launchConfigToRosterJSON(f launchConfig) agentsRosterJSON {
	out := agentsRosterJSON{
		SchemaVersion: agentAdapterSchemaVersion,
		Defaults: agentsDefaultsJSON{
			Agent: f.DefaultAgent,
			Attribution: agentsAttributionJSON{
				Name:  f.Attribution.Name,
				Email: f.Attribution.Email,
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
			ReasoningEffort: a.Effort,
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
func agentsRosterTable(f launchConfig) string {
	var b []byte
	b = append(b, fmt.Sprintf("ward harness roster (schema %d, default agent %q, %d agents)\n",
		agentAdapterSchemaVersion, f.DefaultAgent, len(f.Agents))...)
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

// agentsCommand is Ward's hand-written read-only harness roster group.
func agentsCommand() *cli.Command {
	return &cli.Command{
		Name:  "agents",
		Usage: "the fixed harness adapters: `list` the typed native roster.",
		Commands: []*cli.Command{
			agentsListCommand(),
		},
	}
}

// agentsListCommand builds `ward agents list [--json]`: a read-only dump of the
// typed harness roster (human table, or --json for the JSON surface).
func agentsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "Print the fixed harness roster. --json emits the stable JSON read surface.",
		Description: `list dumps Ward's typed harness adapters, so the roster the binary
launches and the roster it reports cannot drift. The default is a human table; --json emits a
stable, deterministic JSON schema (schema_version, defaults, agents[]) that the
scripts/agent-compat.py consumes as its read surface (ward#417).`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "emit the stable JSON roster instead of the human table"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			cfg, err := loadLaunchConfig()
			if err != nil {
				return fmt.Errorf("ward agents list: load harness config: %w", err)
			}
			w := c.Root().Writer
			if w == nil {
				w = os.Stdout
			}
			if c.Bool("json") {
				buf, err := json.MarshalIndent(launchConfigToRosterJSON(cfg), "", "  ")
				if err != nil {
					return fmt.Errorf("ward agents list: marshal roster: %w", err)
				}
				_, err = io.WriteString(w, string(buf)+"\n")
				return err
			}
			_, err = io.WriteString(w, agentsRosterTable(cfg))
			return err
		},
	}
}
