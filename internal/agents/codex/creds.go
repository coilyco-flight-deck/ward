package codex

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// authEnvKey is the bootstrap-only env channel carrying the base64'd codex
// auth.json from host resolve to in-container write; scrubbed once written.
const authEnvKey = "WARD_CODEX_AUTH_B64"

// codexKeychainService is Codex CLI's macOS generic-password service. The
// account is derived from the canonical CODEX_HOME path below (ward#1641).
const codexKeychainService = "Codex Auth"

// codexKeychainAccount mirrors Codex CLI's stable per-CODEX_HOME account key:
// "cli|" plus the first 16 hex characters of SHA-256(canonical path).
func codexKeychainAccount(codexHome string) string {
	canonical := codexHome
	if resolved, err := filepath.EvalSymlinks(codexHome); err == nil {
		canonical = resolved
	}
	digest := sha256.Sum256([]byte(canonical))
	return "cli|" + hex.EncodeToString(digest[:8])
}

func codexAuthEnvLine(blob string) []agentsapi.EnvLine {
	blob = strings.TrimSpace(blob)
	if blob == "" {
		return nil
	}
	return []agentsapi.EnvLine{{Key: authEnvKey, Value: base64.StdEncoding.EncodeToString([]byte(blob))}}
}

// ResolveCreds prefers ~/.codex/auth.json, then falls back to macOS Keychain.
// Both use the private WARD_CODEX_AUTH_B64 bootstrap line (ward#1641).
func (a Agent) ResolveCreds(hc agentsapi.HostCtx) []agentsapi.EnvLine {
	codexHome := filepath.Join(hc.Home, ".codex")
	path := filepath.Join(codexHome, "auth.json")
	data, fileErr := os.ReadFile(path) // #nosec G304 -- fixed per-user codex creds path
	if fileErr == nil {
		if line := codexAuthEnvLine(string(data)); line != nil {
			return line
		}
	}

	if hc.GOOS != "darwin" || hc.Exec == nil {
		if fileErr != nil {
			hc.Log("ward container: could not read %s (%v); codex will be unauthenticated", path, fileErr)
		}
		return nil
	}

	account := codexKeychainAccount(codexHome)
	out, keychainErr := hc.Exec.Capture(hc.Ctx, "security", "find-generic-password",
		"-s", codexKeychainService, "-a", account, "-w")
	if keychainErr != nil {
		hc.Log("ward container: could not read codex credentials from %s or macOS Keychain (%v); codex will be unauthenticated", path, keychainErr)
		return nil
	}
	if line := codexAuthEnvLine(string(out)); line != nil {
		return line
	}
	hc.Log("ward container: codex credentials from %s and macOS Keychain were empty; codex will be unauthenticated", path)
	return nil
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
