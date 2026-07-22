package goose

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// ollamaHostEnvKey carries the base64'd tower Ollama endpoint resolved host-side;
// decoded here and scrubbed once seeded (the tailnet endpoint is the secret).
const ollamaHostEnvKey = "WARD_GOOSE_OLLAMA_HOST_B64"

// ComposeConfig seeds goose's config.yaml with provider + model (+ the tower
// Ollama host if the host resolved one into the env; ward#186).
func (a Agent) ComposeConfig(rc agentsapi.RunCtx) error {
	dir := filepath.Join(rc.AgentHome, ".config", "goose")
	_ = os.MkdirAll(dir, 0o755)
	provider := envOr("WARD_GOOSE_PROVIDER", "ollama")
	model := strings.TrimSpace(rc.GooseModel)
	if model == "" {
		return fmt.Errorf("missing agent.goose.model: bootstrap must resolve WARD_GOOSE_MODEL or --config agent.goose.model=<model>")
	}
	host := ""
	if b64 := os.Getenv(ollamaHostEnvKey); b64 != "" {
		if dec, derr := base64.StdEncoding.DecodeString(b64); derr == nil {
			host = string(dec)
		}
		_ = os.Unsetenv(ollamaHostEnvKey)
	}
	body := configYAML(provider, model, host)
	out := filepath.Join(dir, "config.yaml")
	if werr := os.WriteFile(out, []byte(body), 0o644); werr != nil { // #nosec G306 -- config, not a secret
		rc.Log("could not write goose config: %v", werr)
		return nil
	}
	if provider == "ollama" && host == "" {
		rc.Log("wrote goose config (provider=%s model=%s) to %s; no tower Ollama host resolved, goose will use its built-in default", provider, model, out)
	} else {
		rc.Log("wrote goose config (provider=%s model=%s) to %s", provider, model, out)
	}
	return nil
}

// configYAML renders goose's config.yaml, matching the bash heredoc lines.
func configYAML(provider, model, host string) string {
	var b strings.Builder
	b.WriteString("# Written by the ward container entrypoint (ward#186): bind goose's provider.\n")
	fmt.Fprintf(&b, "GOOSE_PROVIDER: %s\n", provider)
	fmt.Fprintf(&b, "GOOSE_MODEL: %s\n", model)
	if host != "" {
		fmt.Fprintf(&b, "OLLAMA_HOST: %s\n", host)
	}
	return b.String()
}

// envOr returns the env value for key, or def when unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
