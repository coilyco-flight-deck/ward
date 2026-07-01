package main

// agent_adapter.go projects the embedded fleet roster onto the launcher's adapter
// shape. ward#419 dropped the YAML mirror; see docs/agent-adapter-manifest.md.

import (
	"fmt"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// agentAdapterSchemaVersion is the manifest schema this build understands.
const agentAdapterSchemaVersion = 1

// agentArgv holds the argv prefixes for the three ways ward invokes an agent;
// the prompt (preflight) or seed (headless/interactive) is appended by the caller.
type agentArgv struct {
	Preflight   []string
	Headless    []string
	Interactive []string
}

// agentAdapter is one agent's full divergence record, replacing the per-mode
// Go switches and bash cases. See docs/agent-adapter-manifest.md for the schema.
type agentAdapter struct {
	Name         string
	Binary       string
	ContextLevel int
	Stream       string
	Auth         string
	Argv         agentArgv
}

// preflightArgv returns the host one-shot argv with the prompt appended, plus
// whether one exists. It mirrors containerMode.hostPreflightArgv (ward#152).
func (a agentAdapter) preflightArgv(prompt string) ([]string, bool) {
	if len(a.Argv.Preflight) == 0 {
		return nil, false
	}
	argv := make([]string, 0, len(a.Argv.Preflight)+1)
	argv = append(argv, a.Argv.Preflight...)
	argv = append(argv, prompt)
	return argv, true
}

// agentManifest is the parsed manifest: a schema version plus the agent records.
type agentManifest struct {
	SchemaVersion int
	Agents        []agentAdapter
}

// adapter looks an agent up by name (the --mode value).
func (m agentManifest) adapter(name string) (agentAdapter, bool) {
	for _, a := range m.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return agentAdapter{}, false
}

// loadAgentManifest builds the manifest from the embedded dialect-2 fleet config
// (fleet.go) - the sole source since ward#419 deleted the agent-adapters.yaml mirror.
func loadAgentManifest() (agentManifest, error) {
	f, err := loadFleetConfig()
	if err != nil {
		return agentManifest{}, fmt.Errorf("agent-adapter manifest (from fleet): %w", err)
	}
	m := fleetToAgentManifest(f)
	if err := validateAgentManifest(m); err != nil {
		return agentManifest{}, err
	}
	return m, nil
}

// fleetToAgentManifest projects a parsed fleet roster onto the adapter shape the
// launcher reads (binary/context-level/stream/auth/argv); model/endpoint go direct.
func fleetToAgentManifest(f fleetconfig.Fleet) agentManifest {
	m := agentManifest{SchemaVersion: agentAdapterSchemaVersion}
	for _, a := range f.Agents {
		m.Agents = append(m.Agents, agentAdapter{
			Name:         a.Name,
			Binary:       a.Binary,
			ContextLevel: a.ContextLevel,
			Stream:       a.Stream,
			Auth:         a.Auth,
			Argv: agentArgv{
				Preflight:   a.Argv.Preflight,
				Headless:    a.Argv.Headless,
				Interactive: a.Argv.Interactive,
			},
		})
	}
	return m
}

// validateAgentManifest enforces the schema on the projected fleet roster, so a
// malformed embed fails loud at load instead of driving the wrong binary.
func validateAgentManifest(m agentManifest) error {
	if m.SchemaVersion != agentAdapterSchemaVersion {
		return fmt.Errorf("agent-adapter manifest: schemaVersion %d, want %d", m.SchemaVersion, agentAdapterSchemaVersion)
	}
	if len(m.Agents) == 0 {
		return fmt.Errorf("agent-adapter manifest: no agents defined")
	}
	seen := map[string]bool{}
	for i, a := range m.Agents {
		if a.Name == "" {
			return fmt.Errorf("agent-adapter manifest: agent %d has no name", i)
		}
		if seen[a.Name] {
			return fmt.Errorf("agent-adapter manifest: duplicate agent %q", a.Name)
		}
		seen[a.Name] = true
		if a.Binary == "" {
			return fmt.Errorf("agent-adapter manifest: agent %q has no binary", a.Name)
		}
		if a.ContextLevel < 0 || a.ContextLevel > 2 {
			return fmt.Errorf("agent-adapter manifest: agent %q contextLevel %d out of range 0..2", a.Name, a.ContextLevel)
		}
		if len(a.Argv.Headless) == 0 {
			return fmt.Errorf("agent-adapter manifest: agent %q has no headless argv", a.Name)
		}
		if a.Argv.Headless[0] != a.Binary {
			return fmt.Errorf("agent-adapter manifest: agent %q headless argv starts with %q, not its binary %q", a.Name, a.Argv.Headless[0], a.Binary)
		}
	}
	return nil
}
