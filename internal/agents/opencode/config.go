package opencode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// ComposeConfig writes the opencode config pointing at the local ollama-backed
// model (RunCtx carries the model + endpoint from the entrypoint env).
func (a Agent) ComposeConfig(rc agentsapi.RunCtx) error {
	dir := filepath.Join(rc.AgentHome, ".config", "opencode")
	_ = os.MkdirAll(dir, 0o755)
	body := configJSON(rc.OpencodeModel, rc.OllamaURL)
	out := filepath.Join(dir, "opencode.json")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		rc.Log("could not write opencode config: %v", werr)
		return nil
	}
	rc.Log("wrote qwen-backed opencode config (model ollama/%s via %s) to %s", rc.OpencodeModel, rc.OllamaURL, out)
	return nil
}

// configJSON renders the qwen-backed opencode config; the $schema key is a
// literal, not interpolated.
func configJSON(model, ollamaURL string) string {
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "model": "ollama/%s",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama (local)",
      "options": { "baseURL": "%s" },
      "models": { "%s": {} }
    }
  }
}
`, model, ollamaURL, model)
}
