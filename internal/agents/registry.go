// Package agents is the agentspi.Agent registry (ward#412, Phase 2 of ward#401):
// it wires the four harness packages into a name-keyed map core dispatches
// through, retiring the scattered `switch e.Mode` once call sites cut over
// (Phase 3). The agents it serves are DATA-only - Name/Record/Signer/argv are
// pure; the capability closures are wired by core (cmd/ward) at dispatch.
// See docs/agentspi.md.
package agents

import (
	"github.com/coilyco-flight-deck/ward/internal/agents/claude"
	"github.com/coilyco-flight-deck/ward/internal/agents/codex"
	"github.com/coilyco-flight-deck/ward/internal/agents/goose"
	"github.com/coilyco-flight-deck/ward/internal/agents/opencode"
	"github.com/coilyco-flight-deck/ward/internal/agentspi"
)

// LegacyOpencodeMode is the retired roster key ward#401 renamed to "opencode";
// Lookup keeps it as a back-compat alias. See docs/agentspi.md.
const LegacyOpencodeMode = "qwen"

// Registry builds the name-keyed map of every harness ward drives. The key is
// each agent's roster key (its Name / the --mode value).
func Registry() map[string]agentspi.Agent {
	return map[string]agentspi.Agent{
		"claude":   claude.New(),
		"codex":    codex.New(),
		"opencode": opencode.New(),
		"goose":    goose.New(),
	}
}

// Lookup resolves a --mode value to its agent, translating the legacy "qwen"
// alias to opencode so the rename does not hard-break existing invocations.
func Lookup(mode string) (agentspi.Agent, bool) {
	if mode == LegacyOpencodeMode {
		mode = "opencode"
	}
	a, ok := Registry()[mode]
	return a, ok
}
