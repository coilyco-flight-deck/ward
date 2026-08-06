package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// agent_flags.go wires `ward agent flags`, the generated flag-tree docs page
// for the deliberately selected operational subtree. See docs/agent-flags.md.

const (
	agentFlagsDoc       = "docs/agent-flags.md"
	agentFlagsRegenHint = "make agent-flags"
)

// agentFlagsDocGoal is the doc_goal front-matter the generated page carries so it
// grades against an explicit target like the other docs pages.
const agentFlagsDocGoal = "Give a reader the canonical code-generated flags for Ward's selected operational agent command paths, deliberately omitting self-description and fixed PR or issue leaves documented by their parent contracts."

// agentFlagsMarkdown renders the committed docs/agent-flags.md body: doc_goal,
// generated header, then one section per selected command in the agent subtree.
func agentFlagsMarkdown() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ndoc_goal: %s\n---\n", agentFlagsDocGoal)
	b.WriteString("# ward agent: the flag tree\n\n")
	fmt.Fprintf(&b, "<!-- Generated from the code flag tree by `ward agent flags --markdown` (https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1116); do not edit by hand. Regenerate with `%s`. -->\n\n", agentFlagsRegenHint)
	renderAgentFlagsMarkdown(&b, agentCommand(), []string{"ward", "agent"})
	return b.String(), nil
}

func renderAgentFlagsMarkdown(b *strings.Builder, cmd *cli.Command, path []string) {
	if !agentFlagsRenderPath(path) {
		for _, child := range cmd.Commands {
			nextPath := append(append([]string{}, path...), child.Name)
			renderAgentFlagsMarkdown(b, child, nextPath)
		}
		return
	}
	fmt.Fprintf(b, "## `%s`\n\n", strings.Join(path, " "))
	fmt.Fprintf(b, "- %s\n", agentFlagTreeLine(cmd.Flags))
	for _, child := range cmd.Commands {
		nextPath := append(append([]string{}, path...), child.Name)
		renderAgentFlagsMarkdown(b, child, nextPath)
	}
}

func agentFlagsRenderPath(path []string) bool {
	switch strings.Join(path, " ") {
	case "ward agent roster", "ward agent flags", "ward agent issue", "ward agent pr status", "ward agent pr merge", "ward agent pr rerun":
		return false
	default:
		return true
	}
}

func agentFlagMarkdownLine(f cli.Flag) string {
	names := make([]string, 0, len(f.Names()))
	for _, name := range f.Names() {
		if name == "" {
			continue
		}
		names = append(names, "--"+name)
	}
	line := strings.Join(names, ", ")
	if vf, ok := f.(interface{ IsVisible() bool }); ok && !vf.IsVisible() {
		line = "(hidden) " + line
	}
	return line
}

func agentFlagTreeLine(flags []cli.Flag) string {
	if len(flags) == 0 {
		return "No direct flags."
	}
	lines := make([]string, 0, len(flags))
	for _, f := range flags {
		lines = append(lines, agentFlagMarkdownLine(f))
	}
	return strings.Join(lines, ", ")
}

// agentFlagsCommand builds `ward agent flags`: a read-only self-describe verb
// printing the generated flag tree, or the doc body under --markdown.
func agentFlagsCommand() *cli.Command {
	return &cli.Command{
		Name:  "flags",
		Usage: "Print the agent command flag tree (human sections; --markdown emits docs/agent-flags.md).",
		Description: `flags prints the agent subtree's direct flags as a stable tree. The default is a
human-readable section per command; --markdown (or --format markdown) emits the
exact committed docs/agent-flags.md body, the form ` + "`" + agentFlagsRegenHint + "`" + ` captures.
A drift test fails the build when the code tree and that committed page diverge (ward#1116).`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "markdown", Usage: "emit the committed docs/agent-flags.md body instead of the human tree"},
			&cli.StringFlag{Name: "format", Usage: "output format: tree (default) or markdown"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			markdown := c.Bool("markdown")
			switch strings.ToLower(strings.TrimSpace(c.String("format"))) {
			case "markdown", "md":
				markdown = true
			case "", "tree":
				// keep the --markdown bool's reading
			default:
				return fmt.Errorf("ward agent flags: invalid --format %q: want tree or markdown", c.String("format"))
			}
			render := agentFlagsTable
			if markdown {
				render = agentFlagsMarkdown
			}
			out, err := render()
			if err != nil {
				return fmt.Errorf("ward agent flags: %w", err)
			}
			w := c.Root().Writer
			if w == nil {
				w = os.Stdout
			}
			_, err = fmt.Fprint(w, out)
			return err
		},
	}
}

// agentFlagsTable renders the human tree used by the default command output.
func agentFlagsTable() (string, error) {
	var out strings.Builder
	out.WriteString("ward agent: the flag tree\n\n")
	renderAgentFlagsHuman(&out, agentCommand(), []string{"ward", "agent"})
	return out.String(), nil
}

func renderAgentFlagsHuman(b *strings.Builder, cmd *cli.Command, path []string) {
	if !agentFlagsRenderPath(path) {
		for _, child := range cmd.Commands {
			nextPath := append(append([]string{}, path...), child.Name)
			renderAgentFlagsHuman(b, child, nextPath)
		}
		return
	}
	fmt.Fprintf(b, "%s\n", strings.Join(path, " "))
	fmt.Fprintf(b, "  %s\n\n", agentFlagTreeLine(cmd.Flags))
	for _, child := range cmd.Commands {
		nextPath := append(append([]string{}, path...), child.Name)
		renderAgentFlagsHuman(b, child, nextPath)
	}
}
