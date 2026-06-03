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

// lintCommand is the deprecated alias for `ward doctor allowlist`. Kept for
// one minor release so the cross-repo pre-commit suite keeps passing while
// consumers migrate. Removal tracked in the follow-up referenced in the
// deprecation line. See docs/lint.md.
func lintCommand() *cli.Command {
	return &cli.Command{
		Name:  "lint",
		Usage: "Deprecated alias for `ward doctor allowlist`.",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Fprintln(os.Stderr, "ward lint: deprecated, use `ward doctor allowlist` (alias removed in a future minor)")
			summary, err := runAllowlistCheck()
			if err != nil {
				return err
			}
			fmt.Println(summary)
			return nil
		},
	}
}

// makeTargetHelp matches `target: deps  ## description` lines.
var makeTargetHelp = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:[^=]*?##\s*(.*)$`)

// runAllowlistCheck validates the resolved allowlist against the repo's
// Makefile. Returns a one-line OK summary on success; returns the collected
// problems joined by newlines on failure. The caller decides what to write
// where, so `ward doctor` and the `ward lint` alias share one engine.
func runAllowlistCheck() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	yamlPath, err := resolveConfigPath(explicitConfigPath(), os.Getenv("WARD_CONFIG"), cwd)
	if err != nil {
		return "", err
	}
	repoRoot := filepath.Dir(filepath.Dir(yamlPath))
	makefilePath := filepath.Join(repoRoot, "Makefile")

	verbs, err := loadYamlVerbs(yamlPath)
	if err != nil {
		return "", err
	}
	targets, err := loadMakefileTargets(makefilePath)
	if err != nil {
		return "", err
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
		return "", errors.New(strings.Join(problems, "\n"))
	}
	return fmt.Sprintf("ward doctor allowlist: %d verbs OK", len(verbs)), nil
}

type yamlVerb struct {
	name        string
	run         string
	description string
	line        int
}

func loadYamlVerbs(path string) ([]yamlVerb, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path) // #nosec G304 G703 -- explicit or discovered repo-local config path, cleaned
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
	path = filepath.Clean(path)
	f, err := os.Open(path) // #nosec G304 G703 -- Makefile next to the resolved config, cleaned
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
