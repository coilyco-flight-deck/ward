package main

import (
	"fmt"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
)

// frontierAgentOrder keeps the built-in frontier roster stable and explicit.
var frontierAgentOrder = []string{"claude", "codex", "opencode", "goose"}

// frontierAgentDefaults are ward-local built-ins: the frontier launch shapes the
// bundle may sparsely override, but never define the product defaults for.
var frontierAgentDefaults = map[string]fleetconfig.Agent{
	"claude": {
		Name:         "claude",
		Binary:       "claude",
		ContextLevel: 2,
		Stream:       "stream-json",
		Auth:         "claude-keychain",
		Argv: fleetconfig.Argv{
			Preflight:   []string{"claude", "-p"},
			Headless:    []string{"claude", "-p", "--verbose", "--output-format", "stream-json"},
			Interactive: []string{"claude"},
		},
	},
	"codex": {
		Name:            "codex",
		Binary:          "codex",
		ContextLevel:    1,
		Stream:          "none",
		Auth:            "codex-file",
		Model:           "gpt-5.4",
		ReasoningEffort: "medium",
		Verbosity:       "low",
		Argv: fleetconfig.Argv{
			Headless:    []string{"codex", "exec"},
			Interactive: []string{"codex"},
		},
	},
	"opencode": {
		Name:         "opencode",
		Binary:       "opencode",
		ContextLevel: 0,
		Stream:       "none",
		Auth:         "none",
		Model:        "qwen3-coder:30b",
		Endpoint:     "http://host.docker.internal:8082/v1",
		Argv: fleetconfig.Argv{
			Headless:    []string{"opencode", "run"},
			Interactive: []string{"opencode"},
		},
	},
	"goose": {
		Name:         "goose",
		Binary:       "goose",
		ContextLevel: 2,
		Stream:       "none",
		Auth:         "ollama",
		Argv: fleetconfig.Argv{
			Preflight:   []string{"goose", "run", "-t"},
			Headless:    []string{"goose", "run", "-t"},
			Interactive: []string{"goose", "session"},
		},
	},
}

// frontierAgentNames returns the built-in frontier roster in launch order.
func frontierAgentNames() []string {
	out := make([]string, len(frontierAgentOrder))
	copy(out, frontierAgentOrder)
	return out
}

// mergeAgentOverlay applies a sparse override onto a base frontier definition.
func mergeAgentOverlay(base, override fleetconfig.Agent) fleetconfig.Agent { //nolint:gocyclo,cyclop
	out := base
	if override.Name != "" {
		out.Name = override.Name
	}
	if override.Binary != "" {
		out.Binary = override.Binary
	}
	if override.ContextLevel != -1 {
		out.ContextLevel = override.ContextLevel
	}
	if override.Stream != "" {
		out.Stream = override.Stream
	}
	if override.Auth != "" {
		out.Auth = override.Auth
	}
	if override.Model != "" {
		out.Model = override.Model
	}
	if override.Endpoint != "" {
		out.Endpoint = override.Endpoint
	}
	if override.Provider != "" {
		out.Provider = override.Provider
	}
	if override.ReasoningEffort != "" {
		out.ReasoningEffort = override.ReasoningEffort
	}
	if override.Verbosity != "" {
		out.Verbosity = override.Verbosity
	}
	if len(override.Argv.Preflight) > 0 {
		out.Argv.Preflight = append([]string{}, override.Argv.Preflight...)
	}
	if len(override.Argv.Headless) > 0 {
		out.Argv.Headless = append([]string{}, override.Argv.Headless...)
	}
	if len(override.Argv.Interactive) > 0 {
		out.Argv.Interactive = append([]string{}, override.Argv.Interactive...)
	}
	return out
}

// resolveEffectiveFleet overlays sparse top-level bundle agents onto ward's
// built-in frontier roster, then validates the resulting complete roster.
func resolveEffectiveFleet(raw fleetconfig.Fleet) (fleetconfig.Fleet, error) {
	eff := raw
	eff.Agents = make([]fleetconfig.Agent, 0, len(frontierAgentOrder)+len(raw.Agents))

	rawByName := make(map[string]fleetconfig.Agent, len(raw.Agents))
	for _, a := range raw.Agents {
		rawByName[a.Name] = a
	}
	for _, name := range frontierAgentOrder {
		base, ok := frontierAgentDefaults[name]
		if !ok {
			return fleetconfig.Fleet{}, fmt.Errorf("effective fleet: missing built-in frontier agent %q", name)
		}
		if override, ok := rawByName[name]; ok {
			base = mergeAgentOverlay(base, override)
		}
		eff.Agents = append(eff.Agents, base)
	}
	for _, a := range raw.Agents {
		if _, ok := frontierAgentDefaults[a.Name]; ok {
			continue
		}
		if err := validateResolvedAgent(a); err != nil {
			return fleetconfig.Fleet{}, err
		}
		eff.Agents = append(eff.Agents, a)
	}
	if err := validateResolvedFleet(eff); err != nil {
		return fleetconfig.Fleet{}, err
	}
	return eff, nil
}

// validateResolvedFleet enforces the roster completeness after the merge.
func validateResolvedFleet(f fleetconfig.Fleet) error {
	if len(f.Agents) == 0 {
		return fmt.Errorf("effective fleet: no agents defined")
	}
	seen := map[string]bool{}
	for i, a := range f.Agents {
		if err := validateResolvedAgentAt(a, i, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateResolvedAgent(a fleetconfig.Agent) error {
	return validateResolvedAgentAt(a, -1, nil)
}

func validateResolvedAgentAt(a fleetconfig.Agent, i int, seen map[string]bool) error {
	if a.Name == "" {
		if i >= 0 {
			return fmt.Errorf("effective fleet: agent %d has no name", i)
		}
		return fmt.Errorf("effective fleet: agent has no name")
	}
	if seen != nil {
		if seen[a.Name] {
			return fmt.Errorf("effective fleet: duplicate agent %q", a.Name)
		}
		seen[a.Name] = true
	}
	if a.Binary == "" {
		return fmt.Errorf("effective fleet: agent %q has no binary", a.Name)
	}
	if a.ContextLevel < 0 || a.ContextLevel > 2 {
		return fmt.Errorf("effective fleet: agent %q contextLevel %d out of range 0..2", a.Name, a.ContextLevel)
	}
	if len(a.Argv.Headless) == 0 {
		return fmt.Errorf("effective fleet: agent %q has no headless argv", a.Name)
	}
	if a.Argv.Headless[0] != a.Binary {
		return fmt.Errorf("effective fleet: agent %q headless argv starts with %q, not its binary %q", a.Name, a.Argv.Headless[0], a.Binary)
	}
	return nil
}
