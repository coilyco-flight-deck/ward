package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	kdl "github.com/calico32/kdl-go"
)

func main() {
	var bundleDir string
	var outputPath string
	flag.StringVar(&bundleDir, "bundle", "", "extracted AOS ward-specs bundle directory")
	flag.StringVar(&outputPath, "output", "", "Ward fleet asset to write")
	flag.Parse()

	if strings.TrimSpace(bundleDir) == "" || strings.TrimSpace(outputPath) == "" {
		fmt.Fprintln(os.Stderr, "ward-policy-bake: --bundle and --output are required")
		os.Exit(2)
	}
	if err := bakePolicy(bundleDir, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "ward-policy-bake: %v\n", err)
		os.Exit(1)
	}
}

func bakePolicy(bundleDir, outputPath string) error {
	agents, err := os.ReadFile(filepath.Join(bundleDir, "agents.kdl"))
	if err != nil {
		return fmt.Errorf("read agents.kdl: %w", err)
	}
	roles, err := os.ReadFile(filepath.Join(bundleDir, "roles.kdl"))
	if err != nil {
		return fmt.Errorf("read roles.kdl: %w", err)
	}
	out, err := materializeNativePolicy(agents, roles)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}

func materializeNativePolicy(agentsSource, rolesSource []byte) ([]byte, error) {
	agents, err := uniqueTopLevelNode(agentsSource, "agents")
	if err != nil {
		return nil, fmt.Errorf("agents.kdl: %w", err)
	}
	roles, err := uniqueTopLevelNode(rolesSource, "roles")
	if err != nil {
		return nil, fmt.Errorf("roles.kdl: %w", err)
	}

	fleet := kdl.NewNode("fleet")
	fleet.AddChildren(agents.Children().Nodes...)
	fleetRoles := kdl.NewNode("roles")
	fleetRoles.AddChildren(roles.Children().Nodes...)
	fleet.AddChild(fleetRoles)

	out, err := kdl.EmitToString(kdl.NewDocument(fleet))
	if err != nil {
		return nil, fmt.Errorf("emit native policy: %w", err)
	}
	parsed, err := fleetconfig.Parse([]byte(out))
	if err != nil {
		return nil, fmt.Errorf("validate materialized native policy: %w", err)
	}
	name := strings.TrimSpace(parsed.Defaults.Attribution.Name)
	email := strings.TrimSpace(parsed.Defaults.Attribution.Email)
	if name == "" || email == "" {
		return nil, errors.New("materialized native policy has empty defaults attribution")
	}
	if looksLikeExample(name) || looksLikeExample(email) {
		return nil, fmt.Errorf("materialized native policy retains example attribution %q <%s>", name, email)
	}
	return []byte("// Generated from the pinned AOS ward-specs agents.kdl and roles.kdl.\n" + out), nil
}

func uniqueTopLevelNode(source []byte, name string) (*kdl.Node, error) {
	doc, err := kdl.ParseString(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var found *kdl.Node
	for _, node := range doc.Nodes {
		if node.Name() != name {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("duplicate top-level %q node", name)
		}
		found = node
	}
	if found == nil {
		return nil, fmt.Errorf("missing top-level %q node", name)
	}
	return found, nil
}

func looksLikeExample(value string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), "example")
}
