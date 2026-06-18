package main

// agent_adapter.go reads the aos-published agent-adapter manifest (per-agent
// binary, context level, argv dialect, stream, auth). See docs/agent-adapter-manifest.md.

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed containerassets/agent-adapters.yaml
var agentAdapterAsset embed.FS

// agentAdaptersRel is the embedded manifest, alongside the other container assets.
const agentAdaptersRel = "agent-adapters.yaml"

// agentAdapterSchemaVersion is the manifest schema this build understands.
const agentAdapterSchemaVersion = 1

// agentArgv holds the argv prefixes for the three ways ward invokes an agent;
// the prompt (preflight) or seed (headless/interactive) is appended by the caller.
type agentArgv struct {
	Preflight   []string `yaml:"preflight"`
	Headless    []string `yaml:"headless"`
	Interactive []string `yaml:"interactive"`
}

// agentAdapter is one agent's full divergence record, replacing the per-mode
// Go switches and bash cases. See docs/agent-adapter-manifest.md for the schema.
type agentAdapter struct {
	Name         string    `yaml:"name"`
	Binary       string    `yaml:"binary"`
	ContextLevel int       `yaml:"contextLevel"`
	Stream       string    `yaml:"stream"`
	Auth         string    `yaml:"auth"`
	Argv         agentArgv `yaml:"argv"`
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
	SchemaVersion int            `yaml:"schemaVersion"`
	Agents        []agentAdapter `yaml:"agents"`
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

// loadAgentManifest parses the embedded agent-adapter manifest - the pinned copy
// of the aos-published source, read with no network (mirrors loadSubstrateManifest).
func loadAgentManifest() (agentManifest, error) {
	data, err := agentAdapterAsset.ReadFile("containerassets/" + agentAdaptersRel)
	if err != nil {
		return agentManifest{}, err
	}
	return parseAgentManifest(data)
}

// parseAgentManifest unmarshals and validates the manifest. Validation is strict
// so a malformed or partial manifest fails loudly rather than driving the wrong binary.
func parseAgentManifest(data []byte) (agentManifest, error) {
	var m agentManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return agentManifest{}, fmt.Errorf("agent-adapter manifest: %w", err)
	}
	if m.SchemaVersion != agentAdapterSchemaVersion {
		return agentManifest{}, fmt.Errorf("agent-adapter manifest: schemaVersion %d, want %d", m.SchemaVersion, agentAdapterSchemaVersion)
	}
	if len(m.Agents) == 0 {
		return agentManifest{}, fmt.Errorf("agent-adapter manifest: no agents defined")
	}
	seen := map[string]bool{}
	for i, a := range m.Agents {
		if a.Name == "" {
			return agentManifest{}, fmt.Errorf("agent-adapter manifest: agent %d has no name", i)
		}
		if seen[a.Name] {
			return agentManifest{}, fmt.Errorf("agent-adapter manifest: duplicate agent %q", a.Name)
		}
		seen[a.Name] = true
		if a.Binary == "" {
			return agentManifest{}, fmt.Errorf("agent-adapter manifest: agent %q has no binary", a.Name)
		}
		if a.ContextLevel < 0 || a.ContextLevel > 2 {
			return agentManifest{}, fmt.Errorf("agent-adapter manifest: agent %q contextLevel %d out of range 0..2", a.Name, a.ContextLevel)
		}
		if len(a.Argv.Headless) == 0 {
			return agentManifest{}, fmt.Errorf("agent-adapter manifest: agent %q has no headless argv", a.Name)
		}
		if a.Argv.Headless[0] != a.Binary {
			return agentManifest{}, fmt.Errorf("agent-adapter manifest: agent %q headless argv starts with %q, not its binary %q", a.Name, a.Argv.Headless[0], a.Binary)
		}
	}
	return m, nil
}
