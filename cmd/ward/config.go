package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func documentMapping(doc *yaml.Node, source string) (*yaml.Node, bool, error) {
	if doc.Kind == 0 {
		return nil, false, nil
	}
	if doc.Kind != yaml.DocumentNode {
		return nil, false, fmt.Errorf("parse %s: expected YAML document", source)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind == 0 {
		return nil, false, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("parse %s: top-level config must be a mapping", source)
	}
	return doc.Content[0], true, nil
}

func writeYAMLDocument(path string, doc *yaml.Node) error {
	var body bytes.Buffer
	encoder := yaml.NewEncoder(&body)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		_ = encoder.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if strings.TrimSpace(body.String()) == "" {
		body.WriteString("{}\n")
	}
	if err := os.WriteFile(path, body.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
