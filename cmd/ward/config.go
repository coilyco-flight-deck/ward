package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/config"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

const (
	wardGlobalConfigRefKey       = "config-ref"
	configDropEnvStillSetMessage = wardConfigRefEnv + " is still set in the current process environment"
)

type configDropReport struct {
	configPath         string
	localConfigPresent bool
	localRef           string
	localCleared       bool
	envRef             string
	resultSummary      string
}

func configCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Manage Ward runtime config source selection.",
		Commands: []*cli.Command{
			configDropCommand(),
		},
	}
}

func configDropCommand() *cli.Command {
	return &cli.Command{
		Name:    "drop",
		Aliases: []string{"unset", "reset"},
		Usage:   "Clear the ward-owned local runtime config source selection.",
		Description: strings.Join([]string{
			"drop removes the top-level config-ref value from ~/.ward/config.yaml",
			"without deleting other operator preferences.",
			"",
			"If WARD_CONFIG_REF is inherited by this process, it still wins. The command",
			"reports that environment source and exits non-zero so the result is not",
			"mistaken for the baked/default bundle.",
		}, "\n"),
		Action: func(_ context.Context, _ *cli.Command) error {
			report, err := runConfigDrop()
			printConfigDropReport(os.Stdout, report)
			return err
		},
	}
}

func runConfigDrop() (configDropReport, error) {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return configDropReport{}, err
	}
	report := configDropReport{configPath: path}

	localRef, cleared, present, err := clearLocalConfigRef(path)
	if err != nil {
		return report, err
	}
	report.localRef = localRef
	report.localCleared = cleared
	report.localConfigPresent = present

	report.envRef = strings.TrimSpace(os.Getenv(wardConfigRefEnv))
	report.resultSummary = configDropResultSummary(report.envRef)
	if report.envRef != "" {
		return report, fmt.Errorf("%s; unset it in your shell before expecting Ward to use the baked/default bundle", configDropEnvStillSetMessage)
	}
	return report, nil
}

func clearLocalConfigRef(path string) (ref string, cleared bool, present bool, err error) {
	body, err := os.ReadFile(path) // #nosec G304 -- ~/.ward/config.yaml is the intended operator-local input.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, false, nil
		}
		return "", false, false, fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return "", false, true, fmt.Errorf("parse %s: %w", path, err)
	}
	mapping, hasMapping, err := documentMapping(&doc, path)
	if err != nil {
		return "", false, true, err
	}
	if !hasMapping {
		return "", false, true, nil
	}

	next := make([]*yaml.Node, 0, len(mapping.Content))
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.Value != wardGlobalConfigRefKey {
			next = append(next, key, mapping.Content[i+1])
			continue
		}
		value := mapping.Content[i+1]
		if ref == "" {
			ref = strings.TrimSpace(value.Value)
		}
		cleared = true
	}
	if cleared {
		mapping.Content = next
		if err := writeYAMLDocument(path, &doc); err != nil {
			return ref, false, true, err
		}
		return ref, true, true, nil
	}
	return "", false, true, nil
}

func documentMapping(doc *yaml.Node, path string) (*yaml.Node, bool, error) {
	if doc.Kind == 0 {
		return nil, false, nil
	}
	if doc.Kind != yaml.DocumentNode {
		return nil, false, fmt.Errorf("parse %s: expected YAML document", path)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind == 0 {
		return nil, false, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("parse %s: top-level config must be a mapping", path)
	}
	return doc.Content[0], true, nil
}

func writeYAMLDocument(path string, doc *yaml.Node) error {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if strings.TrimSpace(b.String()) == "" {
		b.WriteString("{}\n")
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func configDropResultSummary(envRef string) string {
	if strings.TrimSpace(envRef) != "" {
		return "inherited environment value: " + wardConfigRefEnv + "=" + envRef
	}
	return "baked/default bundle: " + bakedConfigSource().sourceDesc()
}

func printConfigDropReport(w io.Writer, report configDropReport) {
	if report.configPath != "" {
		_, _ = fmt.Fprintf(w, "ward config drop: config=%s\n", report.configPath)
	}
	switch {
	case report.localCleared:
		if report.localRef == "" {
			_, _ = fmt.Fprintf(w, "ward config drop: cleared %s from local config\n", wardGlobalConfigRefKey)
		} else {
			_, _ = fmt.Fprintf(w, "ward config drop: cleared %s=%s from local config\n", wardGlobalConfigRefKey, report.localRef)
		}
	case report.localConfigPresent:
		_, _ = fmt.Fprintf(w, "ward config drop: no local %s was set\n", wardGlobalConfigRefKey)
	default:
		_, _ = fmt.Fprintln(w, "ward config drop: no local config file was present")
	}
	if report.envRef != "" {
		_, _ = fmt.Fprintf(w, "ward config drop: %s=%s is still inherited by this process\n", wardConfigRefEnv, report.envRef)
	}
	if report.resultSummary != "" {
		_, _ = fmt.Fprintf(w, "ward config drop: resulting config source: %s\n", report.resultSummary)
	}
}
