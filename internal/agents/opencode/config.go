package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// ComposeConfig writes the opencode config pointing at the local Ollama-backed
// model (RunCtx carries the model + endpoint from the entrypoint env).
func (a Agent) ComposeConfig(rc agentsapi.RunCtx) error {
	dir := filepath.Join(rc.AgentHome, ".config", "opencode")
	_ = os.MkdirAll(dir, 0o755)
	body, err := configJSON(rc)
	if err != nil {
		rc.Log("could not render opencode config: %v", err)
		return nil
	}
	out := filepath.Join(dir, "opencode.json")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		rc.Log("could not write opencode config: %v", werr)
		return nil
	}
	rc.Log("wrote opencode config (model ollama/%s via %s) to %s", rc.OpencodeModel, rc.OllamaURL, out)
	return nil
}

type opencodeConfig struct {
	Schema   string                      `json:"$schema"`
	Model    string                      `json:"model"`
	Provider map[string]opencodeProvider `json:"provider"`
}

type opencodeProvider struct {
	NPM     string                    `json:"npm"`
	Name    string                    `json:"name"`
	Options opencodeProviderOptions   `json:"options"`
	Models  map[string]map[string]any `json:"models"`
}

type opencodeProviderOptions struct {
	BaseURL string            `json:"baseURL"`
	Headers map[string]string `json:"headers,omitempty"`
}

// configJSON renders the opencode config; the $schema key is a literal, not
// interpolated.
func configJSON(rc agentsapi.RunCtx) (string, error) {
	cfg := opencodeConfig{
		Schema: "https://opencode.ai/config.json",
		Model:  "ollama/" + rc.OpencodeModel,
		Provider: map[string]opencodeProvider{
			"ollama": {
				NPM:  "@ai-sdk/openai-compatible",
				Name: "agent-proxy",
				Options: opencodeProviderOptions{
					BaseURL: rc.OllamaURL,
					Headers: opencodeHeaders(rc),
				},
				Models: map[string]map[string]any{
					rc.OpencodeModel: {},
				},
			},
		},
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal opencode config: %w", err)
	}
	return string(body) + "\n", nil
}

func opencodeHeaders(rc agentsapi.RunCtx) map[string]string {
	env := rc.Correlation
	headers := map[string]string{}
	for _, pair := range []struct {
		key   string
		value string
	}{
		{key: "x-request-id", value: env.RunID},
		{key: "x-ward-run-id", value: env.RunID},
		{key: "x-ward-container-name", value: env.ContainerName},
		{key: "x-ward-role", value: env.Role},
		{key: "x-ward-harness", value: env.Harness},
		{key: "x-ward-target-repo", value: env.TargetRepo},
		{key: "x-ward-issue-ref", value: env.IssueRef},
		{key: "x-ward-workflow", value: env.Workflow},
		{key: "x-ward-context-level", value: env.ContextLevel},
		{key: "x-ward-version", value: env.Version},
		{key: "x-ward-thread-id", value: env.ThreadID},
	} {
		if v := strings.TrimSpace(pair.value); v != "" {
			headers[pair.key] = v
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}
