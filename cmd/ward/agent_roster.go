package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
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

// agentRoleDefinition carries the role bundle a startup role resolves from.
// It includes the shipped preset fields plus the effective fleet overlay.
type agentRoleDefinition struct {
	Name               string
	Tagline            string
	Capabilities       semanticCapabilitySet
	Modes              string
	DefaultHarness     string
	Posture            agentRolePosture
	ExecutionTimeLimit time.Duration
	ExecutionLimitSet  bool
	Guardfiles         fleetconfig.Guardfiles
	AgentOverlays      map[string]fleetconfig.RoleAgentOverride
	// MergeAuthority lists the workflow modes this role may merge a PR under
	// (ward#1067): embedded product data, never a fleet overlay; absent = never merges.
	MergeAuthority []workflowMode
}

// cloneRoleOverlays copies a role's sparse agent overlay map so callers can
// compose without mutating the parsed fleet config.
func cloneRoleOverlays(in map[string]fleetconfig.RoleAgentOverride) map[string]fleetconfig.RoleAgentOverride {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]fleetconfig.RoleAgentOverride, len(in))
	for name, ov := range in {
		out[name] = ov
	}
	return out
}

// mergeRoleOverlays applies sparse fleet overlays onto a base role overlay map.
func mergeRoleOverlays(base, override map[string]fleetconfig.RoleAgentOverride) map[string]fleetconfig.RoleAgentOverride {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := cloneRoleOverlays(base)
	if out == nil {
		out = map[string]fleetconfig.RoleAgentOverride{}
	}
	for name, ov := range override {
		cur := out[name]
		if strings.TrimSpace(ov.Model) != "" {
			cur.Model = strings.TrimSpace(ov.Model)
		}
		if strings.TrimSpace(ov.Endpoint) != "" {
			cur.Endpoint = strings.TrimSpace(ov.Endpoint)
		}
		if strings.TrimSpace(ov.ReasoningEffort) != "" {
			cur.ReasoningEffort = strings.TrimSpace(ov.ReasoningEffort)
		}
		if strings.TrimSpace(ov.Verbosity) != "" {
			cur.Verbosity = strings.TrimSpace(ov.Verbosity)
		}
		out[name] = cur
	}
	return out
}

// cloneGuardfiles copies a role's guardfile selector so the resolved definition is
// independent of the source fleet config.
func cloneGuardfiles(in fleetconfig.Guardfiles) fleetconfig.Guardfiles {
	out := fleetconfig.Guardfiles{Prefix: in.Prefix}
	if len(in.List) != 0 {
		out.List = append([]string{}, in.List...)
	}
	return out
}

// roleOverlaySummary renders a role's sparse agent overlays in a stable order.
func roleOverlaySummary(overlays map[string]fleetconfig.RoleAgentOverride) string {
	if len(overlays) == 0 {
		return ""
	}
	names := make([]string, 0, len(overlays))
	for name := range overlays {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		ov := overlays[name]
		fields := make([]string, 0, 4)
		if strings.TrimSpace(ov.Model) != "" {
			fields = append(fields, "model="+strings.TrimSpace(ov.Model))
		}
		if strings.TrimSpace(ov.Endpoint) != "" {
			fields = append(fields, "endpoint="+strings.TrimSpace(ov.Endpoint))
		}
		if strings.TrimSpace(ov.ReasoningEffort) != "" {
			fields = append(fields, "reasoning-effort="+strings.TrimSpace(ov.ReasoningEffort))
		}
		if strings.TrimSpace(ov.Verbosity) != "" {
			fields = append(fields, "verbosity="+strings.TrimSpace(ov.Verbosity))
		}
		if len(fields) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s{%s}", name, strings.Join(fields, ", ")))
	}
	return strings.Join(parts, "; ")
}

// agentRoleDefinitionsFromFleet resolves the built-in role presets over the
// effective fleet config's `roles` overlay.
func agentRoleDefinitionsFromFleet(f fleetconfig.Fleet) (map[string]agentRoleDefinition, error) {
	defs := embeddedAgentRoleDefinitions()
	order := embeddedAgentRoleDefinitionOrder()
	for _, role := range f.Roles {
		def, ok := defs[role.Name]
		if !ok {
			return nil, fmt.Errorf("fleet config defines role %q, but ward only registers the shipped embedded presets %q",
				role.Name, strings.Join(order, ", "))
		}
		def.Guardfiles = cloneGuardfiles(role.Guardfiles)
		def.AgentOverlays = mergeRoleOverlays(def.AgentOverlays, role.AgentConfig)
		defs[role.Name] = def
	}
	return defs, nil
}

// agentRoleDefinitions resolves the built-in presets against the live fleet config.
func agentRoleDefinitions() (map[string]agentRoleDefinition, error) {
	fleet, err := loadFleetConfig()
	if err != nil {
		return nil, err
	}
	return agentRoleDefinitionsFromFleet(fleet)
}

// agentMetaCommands are agent subcommands that are NOT startup roles, including
// reservations.
var agentMetaCommands = map[string]bool{"roster": true, "flags": true, "reap": true, "reservations": true, "stop": true, "list": true, "logs": true, "dispatch-health": true, "review": true, "pr": true}

// agentRosterRow is one rendered roster entry: the role, its tagline, its modes, and
// the per-role detail doc it links to.
type agentRosterRow struct {
	Role         string
	Tagline      string
	Capabilities string
	Modes        string
	Doc          string // the per-role detail doc, e.g. agent-engineer.md
}

// agentRosterRows enumerates the live roster: the embedded role defaults resolved
// through the effective fleet config, minus the meta verbs, joined to descriptors.
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
			return nil, fmt.Errorf("agent role %q has no roster descriptor; add it to the embedded role catalog or the resolved fleet role overlays and regenerate %s with `%s`",
				cmd.Name, agentRosterDoc, agentRosterRegenHint)
		}
		modes := info.Modes
		if overlay := roleOverlaySummary(info.AgentOverlays); overlay != "" {
			modes += " Role overlays: " + overlay + "."
		}
		rows = append(rows, agentRosterRow{
			Role:         cmd.Name,
			Tagline:      info.Tagline,
			Capabilities: info.Capabilities.String(),
			Modes:        modes,
			Doc:          "agent-" + cmd.Name + ".md",
		})
	}
	return rows, nil
}

// agentRosterDocGoal is the doc_goal front-matter the generated page carries so it
// grades against an explicit target like every ward doc (ward#289).
const agentRosterDocGoal = "Give a reader the canonical, code-generated list of every ward agent startup role with its tagline, semantic capability preset, and invocation modes, so they can see the ward-owned embedded role defaults plus any effective fleet overlays without the page drifting from the binary."

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
	fmt.Fprintf(&b, "A flat list of every `ward agent` startup role - the roster the binary resolves from\n")
	fmt.Fprintf(&b, "its embedded role defaults plus the effective fleet config's role overlays, so the page can never\n")
	fmt.Fprintf(&b, "drift. Each role is one entry: what the specialist does, what semantic capabilities the\n")
	fmt.Fprintf(&b, "preset carries, and how you invoke it (a ref acts on an issue, freeform text files or answers\n")
	fmt.Fprintf(&b, "it). Run `ward agent roster` (`warded roster`) for this list live at the terminal, and the\n")
	fmt.Fprintf(&b, "per-role docs each entry links to carry the prose detail. See\n")
	fmt.Fprintf(&b, "[agent.md](agent.md) for the umbrella and the `warded` public face.\n\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- [`warded %s`](%s) - %s Capabilities: %s. Modes: %s\n", row.Role, row.Doc, row.Tagline, row.Capabilities, row.Modes)
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
		fmt.Fprintf(&b, "    capabilities: %s\n", row.Capabilities)
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
