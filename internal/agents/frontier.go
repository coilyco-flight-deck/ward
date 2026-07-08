// Package agents also owns ward's built-in frontier launch data. The registry
// packages already own the per-agent records, so the shared roster data lives
// here and core projects it into its effective fleet shape.
package agents

import (
	"github.com/coilyco-flight-deck/ward/internal/agents/claude"
	"github.com/coilyco-flight-deck/ward/internal/agents/codex"
	"github.com/coilyco-flight-deck/ward/internal/agents/goose"
	"github.com/coilyco-flight-deck/ward/internal/agents/opencode"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// frontierManifests is the built-in frontier roster in launch order.
var frontierManifests = []agentsapi.Manifest{
	claude.New().Record(),
	codex.New().Record(),
	opencode.New().Record(),
	goose.New().Record(),
}

// FrontierManifests returns a copy of the built-in frontier roster records in
// launch order.
func FrontierManifests() []agentsapi.Manifest {
	out := make([]agentsapi.Manifest, len(frontierManifests))
	copy(out, frontierManifests)
	return out
}
