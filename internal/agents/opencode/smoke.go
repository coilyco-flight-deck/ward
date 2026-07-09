package opencode

import (
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
	"github.com/coilyco-flight-deck/ward/internal/launchgate/ollamaprobe"
)

// PreLaunchCheck probes the local Ollama endpoint before a headless opencode run
// so an unreachable backend or stale configured model aborts cleanly.
func (a Agent) PreLaunchCheck(rc agentsapi.RunCtx) error {
	return ollamaprobe.PreLaunchModel(rc, "opencode", rc.OllamaURL, rc.OpencodeModel)
}
