// Package agents is the agentsapi.Agent registry (ward#412, Phase 2 of ward#401):
// it wires the four harness packages into a name-keyed map core dispatches
// through, retiring the scattered `switch e.Mode` once call sites cut over
// (Phase 3). The agents it serves own their concrete install and capability
// behavior in the harness packages; core wires them at dispatch.
// See docs/agentsapi.md.
package agents

import (
	"github.com/coilyco-flight-deck/ward/internal/agents/claude"
	"github.com/coilyco-flight-deck/ward/internal/agents/codex"
	"github.com/coilyco-flight-deck/ward/internal/agents/goose"
	"github.com/coilyco-flight-deck/ward/internal/agents/opencode"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// Registry builds the name-keyed map of every harness ward drives. The key is
// each agent's roster key (its Name / the --mode value).
func Registry() map[string]agentsapi.Agent {
	return map[string]agentsapi.Agent{
		"claude":   claude.New(),
		"codex":    codex.New(),
		"opencode": opencode.New(),
		"goose":    goose.New(),
	}
}

// Lookup resolves a --mode value to its agent.
func Lookup(mode string) (agentsapi.Agent, bool) {
	a, ok := Registry()[mode]
	return a, ok
}
