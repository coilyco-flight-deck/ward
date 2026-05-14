package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// lintCommand validates the agent-guard allowlist against the repo's
// Makefile so the verb surface and the make-target surface cannot drift.
// Rules:
//
//   - commands.<verb>.run must equal "make <verb>".
//   - The Makefile must declare a target named <verb>.
//   - The verb description must equal the Makefile target's `## desc`
//     auto-help comment.
func lintCommand() *cli.Command {
	return &cli.Command{
		Name:  "lint",
		Usage: "Lint .agent-guard/agent-guard.yaml (or .coily/coily.yaml) against the repo Makefile.",
		Action: func(_ context.Context, _ *cli.Command) error {
			return runLint()
		},
	}
}

// makeTargetHelp matches `target: deps  ## description` lines.
var makeTargetHelp = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:[^=]*?##\s*(.*)$`)

func runLint() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	yamlPath, err := discoverConfig(cwd)
	if err != nil {
		return err
	}
	repoRoot := filepath.Dir(filepath.Dir(yamlPath))
	makefilePath := filepath.Join(repoRoot, "Makefile")

	verbs, err := loadYamlVerbs(yamlPath)
	if err != nil {
		return err
	}
	targets, err := loadMakefileTargets(makefilePath)
	if err != nil {
		return err
	}

	var problems []string
	for _, v := range verbs {
		want := "make " + v.name
		if v.run != want {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: commands.%s.run = %q, want %q",
				yamlPath, v.line, v.name, v.run, want))
		}
		t, ok := targets[v.name]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: commands.%s has no matching Makefile target",
				yamlPath, v.line, v.name))
			continue
		}
		if v.description != t.description {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: commands.%s.description = %q, want %q (from %s:%d)",
				yamlPath, v.line, v.name, v.description, t.description,
				makefilePath, t.line))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	fmt.Printf("agent-guard lint: %d verbs OK\n", len(verbs))
	return nil
}

type yamlVerb struct {
	name        string
	run         string
	description string
	line        int
}

func loadYamlVerbs(path string) ([]yamlVerb, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- discovered repo-local config path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	commands, err := findCommandsNode(path, &root)
	if err != nil {
		return nil, err
	}
	verbs := make([]yamlVerb, 0, len(commands.Content)/2)
	for i := 0; i+1 < len(commands.Content); i += 2 {
		k, v := commands.Content[i], commands.Content[i+1]
		if v.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s:%d: commands.%s is not a mapping", path, k.Line, k.Value)
		}
		verbs = append(verbs, parseVerbNode(k, v))
	}
	return verbs, nil
}

func findCommandsNode(path string, root *yaml.Node) (*yaml.Node, error) {
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: top level is not a mapping", path)
	}
	doc := root.Content[0]
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "commands" {
			continue
		}
		commands := doc.Content[i+1]
		if commands.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s: 'commands' is not a mapping", path)
		}
		return commands, nil
	}
	return nil, fmt.Errorf("%s: missing top-level 'commands:' map", path)
}

func parseVerbNode(key, value *yaml.Node) yamlVerb {
	verb := yamlVerb{name: key.Value, line: key.Line}
	for j := 0; j+1 < len(value.Content); j += 2 {
		switch value.Content[j].Value {
		case "run":
			verb.run = value.Content[j+1].Value
		case "description":
			verb.description = value.Content[j+1].Value
		}
	}
	return verb
}

type makeTarget struct {
	name        string
	description string
	line        int
}

func loadMakefileTargets(path string) (map[string]makeTarget, error) {
	f, err := os.Open(path) // #nosec G304 -- discovered repo-local Makefile
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close() // #nosec G307 -- read-only file handle
	out := make(map[string]makeTarget)
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		m := makeTargetHelp.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		out[m[1]] = makeTarget{name: m[1], description: strings.TrimSpace(m[2]), line: lineNo}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}
