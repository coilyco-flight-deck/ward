package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// agent_roster.go is `ward agent roster`: the binary printing its own startup-role
// roster as a flat list (ward#348). See docs/agent-roster.md.

// agentRosterDoc is the committed flat-list page the roster regenerates; the make
// `agent-roster` target writes it and the drift test guards it.
const agentRosterDoc = "docs/agent-roster.md"

// agentRosterRegenHint names the command that rewrites agentRosterDoc, quoted in the
// generated header and drift-test failures so a red build is self-curing.
const agentRosterRegenHint = "make agent-roster"

type agentRolePosture string

const (
	agentRolePostureDetached    agentRolePosture = "detached"
	agentRolePostureAttached    agentRolePosture = "attached"
	agentRolePostureNoCode      agentRolePosture = "no-code"
	agentRolePostureCodeLanding agentRolePosture = "code-landing"
)

// agentRoleDefinition describes one fixed workflow, not an authorization
// principal. See docs/agent-roles.md.
type agentRoleDefinition struct {
	Name               string
	Tagline            string
	Modes              string
	DefaultHarness     string
	Posture            agentRolePosture
	ExecutionTimeLimit time.Duration
	ExecutionLimitSet  bool
}

func fixedRoleExecutionLimit(role string) time.Duration {
	switch strings.TrimSpace(role) {
	case roleEngineer:
		return 90 * time.Minute
	case roleQA:
		return 30 * time.Minute
	default:
		return 0
	}
}

func embeddedAgentRoleDefinitionOrder() []string {
	return []string{roleEngineer, roleDirector, roleQA}
}

// agentRoleDefinitions returns the fixed command workflow descriptors.
func agentRoleDefinitions() (map[string]agentRoleDefinition, error) {
	defaultHarness, err := selectedAgentMode()
	if err != nil {
		return nil, err
	}
	harness := string(defaultHarness)
	return map[string]agentRoleDefinition{
		roleEngineer: {
			Name: roleEngineer, Tagline: "Implements a ticket end to end.",
			Modes:          "A ref carries that issue detached. Freeform text files an issue first, then carries it.",
			DefaultHarness: harness, Posture: agentRolePostureCodeLanding,
			ExecutionTimeLimit: fixedRoleExecutionLimit(roleEngineer), ExecutionLimitSet: true,
		},
		roleDirector: {
			Name: roleDirector, Tagline: "Opens the read-only director surface. Autonomous burndown is opt-in.",
			Modes:          "Attached read-only control surface over a repo backlog. Use --burndown or --drain for the autonomous heartbeat.",
			DefaultHarness: harness, Posture: agentRolePostureAttached,
		},
		roleQA: {
			Name: roleQA, Tagline: "Inspects a candidate and posts a structured verdict comment.",
			Modes:          "A ref inspects the issue, branch, pull request, and checks, then posts a structured QA verdict.",
			DefaultHarness: harness, Posture: agentRolePostureNoCode,
			ExecutionTimeLimit: fixedRoleExecutionLimit(roleQA), ExecutionLimitSet: true,
		},
	}, nil
}

// agentMetaCommands are agent subcommands that are NOT startup roles, including
// reservations.
var agentMetaCommands = map[string]bool{"run": true, "message": true, "cluster": true, "roster": true, "flags": true, "reap": true, "reservations": true, "stop": true, "list": true, "logs": true, "issue": true, "dispatch-health": true, "review": true, "pr": true, "recover": true}

// agentRosterRow is one rendered roster entry: the role, its tagline, its modes, and
// the per-role detail doc it links to.
type agentRosterRow struct {
	Role    string
	Tagline string
	Modes   string
	Doc     string // the per-role detail doc, e.g. agent-engineer.md
}

// agentRosterRows enumerates the fixed workflow roster, minus the meta verbs,
// joined to typed descriptors.
func agentRosterRows() ([]agentRosterRow, error) {
	defs, err := agentRoleDefinitions()
	if err != nil {
		return nil, err
	}
	return agentRosterRowsFromDefinitions(agentCommand().Commands, defs)
}

// agentRosterRowsFromDefinitions is the pure core over an explicit command set and
// resolved role definitions. Missing descriptors stay a hard error.
func agentRosterRowsFromDefinitions(cmds []*cli.Command, defs map[string]agentRoleDefinition) ([]agentRosterRow, error) {
	var rows []agentRosterRow
	for _, cmd := range cmds {
		if agentMetaCommands[cmd.Name] {
			continue
		}
		info, ok := defs[cmd.Name]
		if !ok {
			return nil, fmt.Errorf("agent workflow %q has no fixed roster descriptor; update the command-local roster and regenerate %s with `%s`",
				cmd.Name, agentRosterDoc, agentRosterRegenHint)
		}
		rows = append(rows, agentRosterRow{
			Role: cmd.Name, Tagline: info.Tagline, Modes: info.Modes,
			Doc: "agent-" + cmd.Name + ".md",
		})
	}
	return rows, nil
}

// agentRosterDocGoal is the doc_goal front-matter the generated page carries so it
// grades against an explicit target like every ward doc (ward#289).
const agentRosterDocGoal = "Give a reader the canonical, code-generated list of Ward's fixed agent workflows with each command's purpose and invocation modes, without presenting roles as security principals."

// agentRosterMarkdown renders the committed docs/agent-roster.md body: doc_goal
// front-matter plus a flat bullet list (not a table, per the house Voice rules).
func agentRosterMarkdown() (string, error) {
	rows, err := agentRosterRows()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\ndoc_goal: %s\n---\n", agentRosterDocGoal)
	fmt.Fprintf(&b, "# ward agent: the role roster\n\n")
	fmt.Fprintf(&b, "<!-- Generated from the code roster by `ward agent roster --markdown` (ward#348); do not edit by hand. Regenerate with `%s`. -->\n\n", agentRosterRegenHint)
	fmt.Fprintf(&b, "A flat list of every fixed `ward agent` workflow registered by the binary. A role\n")
	fmt.Fprintf(&b, "word selects workflow mechanics only. It never grants broker operations, credentials,\n")
	fmt.Fprintf(&b, "mounts, network reach, models, identity, or merge authority. Run `ward agent roster`\n")
	fmt.Fprintf(&b, "(`warded roster`) for this list live at the terminal, and the\n")
	fmt.Fprintf(&b, "per-role docs each entry links to carry the prose detail. See\n")
	fmt.Fprintf(&b, "[agent.md](agent.md) for the umbrella and the `warded` public face.\n\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- [`warded %s`](%s) - %s Modes: %s\n", row.Role, row.Doc, row.Tagline, row.Modes)
	}
	fmt.Fprintf(&b, "\n## See also\n\n")
	fmt.Fprintf(&b, "- [agent.md](agent.md) - the `ward agent` umbrella and the `warded` public face.\n")
	fmt.Fprintf(&b, "- [agent-subcommands.md](agent-subcommands.md) - the roles compared, the pre-flight, the reaper backstop.\n")
	return b.String(), nil
}

// agentRosterTable renders the human terminal table (the default): one block per role
// with its tagline, modes, and detail doc.
func agentRosterTable() (string, error) {
	rows, err := agentRosterRows()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ward agent: the fixed workflow roster (%d workflows)\n", len(rows))
	for _, row := range rows {
		fmt.Fprintf(&b, "\n  warded %s - %s\n", row.Role, row.Tagline)
		fmt.Fprintf(&b, "    modes: %s\n", row.Modes)
		fmt.Fprintf(&b, "    docs:  docs/%s\n", row.Doc)
	}
	return b.String(), nil
}

// agentRosterDefaultFooter orients a bare `warded` after the roster: how to launch a
// role or hand a bare ref to the engineer (ward#360). It names no role itself.
const agentRosterDefaultFooter = "\nPick a role above, or hand a bare ref straight to the engineer:\n" +
	"  warded <ref>         # e.g. warded #98 or owner/repo#N - the engineer default\n" +
	"  warded <role> ...    # send in a specific role from the roster above\n" +
	"  ward agent roster    # reprint this roster on its own\n"

// agentRosterDefault prints the roster table + launch-hint footer for a truly-empty
// `warded` (ward#360), reusing the ward#348 source so it auto-tracks registered roles.
func agentRosterDefault(c *cli.Command) error {
	table, err := agentRosterTable()
	if err != nil {
		return fmt.Errorf("ward agent: %w", err)
	}
	w := c.Root().Writer
	if w == nil {
		w = os.Stdout
	}
	_, err = io.WriteString(w, table+agentRosterDefaultFooter)
	return err
}

// agentRosterCommand builds `ward agent roster`: a read-only self-describe verb (no
// audit/clean-tree gate) printing the roster - table, or the doc body under --markdown.
func agentRosterCommand() *cli.Command {
	return &cli.Command{
		Name:  "roster",
		Usage: "Print the flat list of every fixed agent workflow the binary registers (human table; --markdown emits docs/agent-roster.md).",
		Description: `roster prints the fixed workflow roster by walking the commands ` + "`ward agent`" + ` itself
registers - the binary describing its own team. The default is a human-readable
table; --markdown (or --format markdown) emits the exact committed docs/agent-roster.md
body, the form ` + "`" + agentRosterRegenHint + "`" + ` captures. A drift test fails the build when the
code roster and that committed page diverge (ward#348).`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "markdown", Usage: "emit the committed docs/agent-roster.md body instead of the human table"},
			&cli.StringFlag{Name: "format", Usage: "output format: table (default) or markdown"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			markdown := c.Bool("markdown")
			switch strings.ToLower(strings.TrimSpace(c.String("format"))) {
			case "markdown", "md":
				markdown = true
			case "", "table":
				// keep the --markdown bool's reading
			default:
				return fmt.Errorf("ward agent roster: invalid --format %q: want table or markdown", c.String("format"))
			}
			render := agentRosterTable
			if markdown {
				render = agentRosterMarkdown
			}
			out, err := render()
			if err != nil {
				return fmt.Errorf("ward agent roster: %w", err)
			}
			w := c.Root().Writer
			if w == nil {
				w = os.Stdout
			}
			_, err = io.WriteString(w, out)
			return err
		},
	}
}
