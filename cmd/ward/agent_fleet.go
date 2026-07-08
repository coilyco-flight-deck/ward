package main

import (
	"fmt"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// frontierAgentOrder keeps the built-in frontier roster stable and explicit.
var frontierAgentOrder = frontierNamesFromManifests(agents.FrontierManifests())

// frontierAgentDefaults are ward-local built-ins: the frontier launch shapes the
// bundle may sparsely override, but never define the product defaults for.
var frontierAgentDefaults = frontierDefaultsFromManifests(agents.FrontierManifests())

// frontierAgentNames returns the built-in frontier roster in launch order.
func frontierAgentNames() []string {
	out := make([]string, len(frontierAgentOrder))
	copy(out, frontierAgentOrder)
	return out
}

func frontierNamesFromManifests(ms []agentsapi.Manifest) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

func frontierDefaultsFromManifests(ms []agentsapi.Manifest) map[string]fleetconfig.Agent {
	out := make(map[string]fleetconfig.Agent, len(ms))
	for _, m := range ms {
		out[m.Name] = fleetconfig.Agent{
			Name:            m.Name,
			Binary:          m.Binary,
			ContextLevel:    m.ContextLevel,
			Stream:          m.Stream,
			Auth:            m.Auth,
			Model:           m.Model,
			Endpoint:        m.Endpoint,
			ReasoningEffort: m.ReasoningEffort,
			Verbosity:       m.Verbosity,
			Argv: fleetconfig.Argv{
				Preflight:   append([]string{}, m.Argv.Preflight...),
				Headless:    append([]string{}, m.Argv.Headless...),
				Interactive: append([]string{}, m.Argv.Interactive...),
			},
		}
	}
	return out
}

// mergeAgentOverlay applies a sparse top-level agent override onto a base
// frontier definition. Empty fields keep the base values.
func mergeAgentOverlay(base, override fleetconfig.Agent) fleetconfig.Agent {
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
