package goose

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
	"github.com/coilyco-flight-deck/ward/internal/launchgate/ollamaprobe"
)

// defaultGooseOllamaHost is goose's built-in Ollama endpoint, used when no tower
// host was seeded into config.yaml - it is what goose itself would dial by default.
const defaultGooseOllamaHost = "http://localhost:11434"

// PreLaunchCheck probes goose's configured Ollama endpoint before a headless run so
// an unreachable local model aborts clean instead of hanging (ward#487).
func (a Agent) PreLaunchCheck(rc agentsapi.RunCtx) error {
	return ollamaprobe.PreLaunch(rc, "goose", configuredOllamaHost(rc))
}

// configuredOllamaHost reads the OLLAMA_HOST line ComposeConfig wrote (the endpoint
// goose will dial), falling back to goose's built-in localhost default.
func configuredOllamaHost(rc agentsapi.RunCtx) string {
	path := filepath.Join(rc.AgentHome, ".config", "goose", "config.yaml")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed per-run config path under AgentHome
	if err != nil {
		return defaultGooseOllamaHost
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "OLLAMA_HOST:"); ok {
			if h := strings.TrimSpace(rest); h != "" {
				return h
			}
		}
	}
	return defaultGooseOllamaHost
}
