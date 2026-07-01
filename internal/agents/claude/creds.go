package claude

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// credsEnvKey is the bootstrap-only env channel carrying the base64'd claude
// OAuth blob from host resolve to in-container write; scrubbed once written.
const credsEnvKey = "WARD_CLAUDE_CREDS_B64"

// claudeKeychainService is the macOS login-keychain service holding the Max/Pro
// OAuth credential Claude Code reads; resolved on the host, never in the image.
const claudeKeychainService = "Claude Code-credentials"

// ResolveCreds runs host-side: read the claude OAuth blob (macOS keychain, else
// the dotfile) as the base64'd WARD_CLAUDE_CREDS_B64 env-file line (docs/agent.md).
func (a Agent) ResolveCreds(hc agentsapi.HostCtx) []agentsapi.EnvLine {
	var blob string
	if hc.GOOS == "darwin" {
		out, err := hc.Exec.Capture(hc.Ctx, "security", "find-generic-password",
			"-s", claudeKeychainService, "-w")
		if err != nil {
			hc.Log("ward container: could not read claude credentials from keychain (%v); claude will be unauthenticated", err)
			return nil
		}
		blob = strings.TrimSpace(string(out))
	} else {
		path := filepath.Join(hc.Home, ".claude", ".credentials.json")
		data, err := os.ReadFile(path) // #nosec G304 -- fixed per-user claude creds path
		if err != nil {
			hc.Log("ward container: could not read %s (%v); claude will be unauthenticated", path, err)
			return nil
		}
		blob = strings.TrimSpace(string(data))
	}
	// Early heads-up only (ward#222): the blob still ships - the in-container auth
	// smoke test is the hard gate - but the cause is now visible host-side.
	if ok, reason := claudeCredsHealth(blob, time.Now()); !ok {
		hc.Log("ward container: claude credentials look unusable (%s); the in-container auth smoke test will fail loudly (ward#222)", reason)
	}
	if blob == "" {
		return nil
	}
	return []agentsapi.EnvLine{{Key: credsEnvKey, Value: base64.StdEncoding.EncodeToString([]byte(blob))}}
}

// claudeCredsHealth flags a clearly-unusable claude OAuth blob (empty, no access
// token, expired) before it ships into a container (ward#222). Pure + testable.
func claudeCredsHealth(blob string, now time.Time) (ok bool, reason string) {
	blob = strings.TrimSpace(blob)
	if blob == "" {
		return false, "empty credentials"
	}
	var parsed struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"`
	}
	if err := json.Unmarshal([]byte(blob), &parsed); err != nil {
		return true, "" // unrecognised shape; defer to the smoke test
	}
	token, exp := parsed.ClaudeAiOauth.AccessToken, parsed.ClaudeAiOauth.ExpiresAt
	if token == "" {
		token, exp = parsed.AccessToken, parsed.ExpiresAt
	}
	if token == "" {
		return false, "no accessToken in credentials"
	}
	if exp > 0 && now.After(time.UnixMilli(exp)) {
		return false, fmt.Sprintf("access token expired %s ago (re-run 'claude' on the host to refresh)", now.Sub(time.UnixMilli(exp)).Round(time.Second))
	}
	return true, ""
}

// WriteCreds runs in-container: decode the host-injected base64 blob into
// ~/.claude/.credentials.json, then scrub the env channel (ward#357).
func (a Agent) WriteCreds(rc agentsapi.RunCtx) error {
	b64 := os.Getenv(credsEnvKey)
	if b64 == "" {
		rc.Log("no claude credentials injected; claude will be unauthenticated")
		return nil
	}
	dir := filepath.Join(rc.AgentHome, ".claude")
	_ = os.MkdirAll(dir, 0o755)
	dec, derr := base64.StdEncoding.DecodeString(b64)
	if derr != nil {
		rc.Log("could not decode claude credentials: %v", derr)
		return nil
	}
	out := filepath.Join(dir, ".credentials.json")
	if werr := os.WriteFile(out, dec, 0o600); werr != nil {
		rc.Log("could not write claude credentials: %v", werr)
		return nil
	}
	_ = os.Unsetenv(credsEnvKey)
	rc.Log("wrote claude credentials to %s (scrubbed %s from env)", out, credsEnvKey)
	return nil
}
