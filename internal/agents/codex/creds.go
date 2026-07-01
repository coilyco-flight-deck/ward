package codex

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// authEnvKey is the bootstrap-only env channel carrying the base64'd codex
// auth.json from host resolve to in-container write; scrubbed once written.
const authEnvKey = "WARD_CODEX_AUTH_B64"

// ResolveCreds runs host-side: read ~/.codex/auth.json as the base64'd
// WARD_CODEX_AUTH_B64 env-file line (best-effort; docs/agent.md).
func (a Agent) ResolveCreds(hc agentsapi.HostCtx) []agentsapi.EnvLine {
	path := filepath.Join(hc.Home, ".codex", "auth.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed per-user codex creds path
	if err != nil {
		hc.Log("ward container: could not read %s (%v); codex will be unauthenticated", path, err)
		return nil
	}
	blob := strings.TrimSpace(string(data))
	if blob == "" {
		return nil
	}
	return []agentsapi.EnvLine{{Key: authEnvKey, Value: base64.StdEncoding.EncodeToString([]byte(blob))}}
}

// WriteCreds runs in-container: decode the host-injected base64 auth.json into
// ~/.codex/auth.json, then scrub the env channel (ward#357).
func (a Agent) WriteCreds(rc agentsapi.RunCtx) error {
	b64 := os.Getenv(authEnvKey)
	if b64 == "" {
		rc.Log("no codex credentials injected; codex will be unauthenticated (run 'codex login' on the host to seed ~/.codex/auth.json)")
		return nil
	}
	dir := filepath.Join(rc.AgentHome, ".codex")
	_ = os.MkdirAll(dir, 0o755)
	dec, derr := base64.StdEncoding.DecodeString(b64)
	if derr != nil {
		rc.Log("could not decode codex credentials: %v", derr)
		return nil
	}
	out := filepath.Join(dir, "auth.json")
	if werr := os.WriteFile(out, dec, 0o600); werr != nil {
		rc.Log("could not write codex credentials: %v", werr)
		return nil
	}
	_ = os.Unsetenv(authEnvKey)
	rc.Log("wrote codex credentials to %s (scrubbed %s from env)", out, authEnvKey)
	return nil
}
