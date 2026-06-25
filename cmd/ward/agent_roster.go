package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

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

// agentRoleInfo carries the flat-list columns a cli.Command does not: the "what this
// specialist does" tagline and the ref-vs-freeform invocation note.
type agentRoleInfo struct {
	Tagline string // the one-line "what this specialist does"
	Modes   string // ref vs freeform; detached vs interactive
}

// agentRoleInfos holds the per-role columns keyed by the registered role name. A
// newly-registered role with no entry is a hard error (agentRosterRowsFrom).
var agentRoleInfos = map[string]agentRoleInfo{
	"engineer": {
		Tagline: "Implements a ticket end to end.",
		Modes:   "A ref carries that issue (detached fire-and-forget; `--watch` attaches); freeform text files an issue first, then carries it.",
	},
	"architect": {
		Tagline: "Reads the clone, scopes and dispatches work - but cannot push.",
		Modes:   "Seedless read-only interactive session; no ref, no issue.",
	},
	"director": {
		Tagline: "Autonomously drives a repo's headless lane to drain.",
		Modes:   "Autonomous supervised loop over a repo's backlog (`--repo` scope); no ref.",
	},
	"advisor": {
		Tagline: "Answers without writing code.",
		Modes:   "A ref researches the issue and posts the answer as a comment; freeform text answers inline.",
	},
}

// agentMetaCommands are agent subcommands that are NOT startup roles (the self-describe
// verbs like `roster`); the roster enumeration skips them.
var agentMetaCommands = map[string]bool{"roster": true}

// agentRosterRow is one rendered roster entry: the role, its tagline, its modes, and
// the per-role detail doc it links to.
type agentRosterRow struct {
	Role    string
	Tagline string
	Modes   string
	Doc     string // the per-role detail doc, e.g. agent-engineer.md
}

// agentRosterRows enumerates the live roster: the roles agentCommand() registers,
// minus the meta verbs, joined to their descriptors (ward#348).
func agentRosterRows() ([]agentRosterRow, error) {
	return agentRosterRowsFrom(agentCommand().Commands)
}

// agentRosterRowsFrom is the pure core over an explicit command set (testable). A
// registered non-meta role missing from agentRoleInfos is a hard error.
func agentRosterRowsFrom(cmds []*cli.Command) ([]agentRosterRow, error) {
	var rows []agentRosterRow
	for _, cmd := range cmds {
		if agentMetaCommands[cmd.Name] {
			continue
		}
		info, ok := agentRoleInfos[cmd.Name]
		if !ok {
			return nil, fmt.Errorf("agent role %q has no roster descriptor; add it to agentRoleInfos in cmd/ward/agent_roster.go and regenerate %s with `%s`",
				cmd.Name, agentRosterDoc, agentRosterRegenHint)
		}
		rows = append(rows, agentRosterRow{
			Role:    cmd.Name,
			Tagline: info.Tagline,
			Modes:   info.Modes,
			Doc:     "agent-" + cmd.Name + ".md",
		})
	}
	return rows, nil
}

// agentRosterMarkdown renders the committed docs/agent-roster.md body: a flat table,
// one row per role, each linking to its per-role doc.
func agentRosterMarkdown() (string, error) {
	rows, err := agentRosterRows()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# ward agent: the role roster\n\n")
	fmt.Fprintf(&b, "<!-- Generated from the code roster by `ward agent roster --markdown` (ward#348); do not edit by hand. Regenerate with `%s`. -->\n\n", agentRosterRegenHint)
	fmt.Fprintf(&b, "A flat list of every `ward agent` startup role - the roster `agentCommand()` registers in\n")
	fmt.Fprintf(&b, "code, rendered by the binary describing itself so the page can never drift. Each role is one\n")
	fmt.Fprintf(&b, "entry: what the specialist does and how you invoke it (a ref acts on an issue, freeform text\n")
	fmt.Fprintf(&b, "files or answers it). Run `ward agent roster` (`warded roster`) for this list live at the\n")
	fmt.Fprintf(&b, "terminal; the per-role docs each row links to carry the prose detail. See\n")
	fmt.Fprintf(&b, "[agent.md](agent.md) for the umbrella and the `warded` public face.\n\n")
	fmt.Fprintf(&b, "| Role | What this specialist does | Invocation modes |\n")
	fmt.Fprintf(&b, "| --- | --- | --- |\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| [`warded %s`](%s) | %s | %s |\n", row.Role, row.Doc, row.Tagline, row.Modes)
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
	fmt.Fprintf(&b, "ward agent: the startup-role roster (%d roles)\n", len(rows))
	for _, row := range rows {
		fmt.Fprintf(&b, "\n  warded %s - %s\n", row.Role, row.Tagline)
		fmt.Fprintf(&b, "    modes: %s\n", row.Modes)
		fmt.Fprintf(&b, "    docs:  docs/%s\n", row.Doc)
	}
	return b.String(), nil
}

// agentRosterCommand builds `ward agent roster`: a read-only self-describe verb (no
// audit/clean-tree gate) printing the roster - table, or the doc body under --markdown.
func agentRosterCommand() *cli.Command {
	return &cli.Command{
		Name:  "roster",
		Usage: "Print the flat list of every agent role the binary registers (human table; --markdown emits docs/agent-roster.md).",
		Description: `roster prints the startup-role roster by walking the roles ` + "`ward agent`" + ` itself
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
