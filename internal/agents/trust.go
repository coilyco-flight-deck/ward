package agents

import (
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// trust.go classifies a harness cloud vs self-hosted from its manifest, the signal
// ward's host one-shot trust gate keys off (ward#162). See docs/agent-lifecycle.md.

// LocalModel reports a self-hosted / local model (ollama / none / unset auth, or a
// local endpoint) vs a trusted cloud credential; unknown auth reads as local (ward#162).
func LocalModel(rec agentsapi.Manifest) bool {
	switch rec.Auth {
	case "ollama", "none", "":
		return true
	}
	return strings.TrimSpace(rec.Endpoint) != ""
}
