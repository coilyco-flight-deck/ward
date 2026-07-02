package opencode

import (
	"github.com/coilyco-flight-deck/ward/internal/agents/ollamaprobe"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// PreLaunchCheck probes the local Ollama endpoint before a headless opencode run so
// an unreachable model backend aborts clean instead of hanging (ward#487).
func (a Agent) PreLaunchCheck(rc agentsapi.RunCtx) error {
	return ollamaprobe.PreLaunch(rc, "opencode", rc.OllamaURL)
}
