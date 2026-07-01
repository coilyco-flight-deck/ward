package claude

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestWriteCredsScrubsEnv: WriteCreds writes ~/.claude/.credentials.json then
// scrubs the bootstrap-only WARD_CLAUDE_CREDS_B64 env var (ward#357).
func TestWriteCredsScrubsEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv(credsEnvKey, base64.StdEncoding.EncodeToString([]byte(`{"ok":1}`)))
	rc := agentsapi.RunCtx{AgentHome: home, Log: noopLog}
	if err := (Agent{}).WriteCreds(rc); err != nil {
		t.Fatalf("WriteCreds: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", ".credentials.json")); err != nil {
		t.Fatalf("expected ~/.claude/.credentials.json written: %v", err)
	}
	if v := os.Getenv(credsEnvKey); v != "" {
		t.Errorf("%s should be scrubbed after seeding, got %q", credsEnvKey, v)
	}
}

// TestResolveCredsFromDotfile: on a non-darwin host, ResolveCreds reads the
// ~/.claude/.credentials.json dotfile and returns it base64'd on its own line.
func TestResolveCredsFromDotfile(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	const blob = `{"claudeAiOauth":{"accessToken":"tok"}}`
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(blob), 0o600); err != nil {
		t.Fatal(err)
	}
	hc := agentsapi.HostCtx{Ctx: context.Background(), GOOS: "linux", Home: home,
		Exec: &shell.Runner{Stderr: io.Discard}, Log: noopLog}
	lines := (Agent{}).ResolveCreds(hc)
	if len(lines) != 1 || lines[0].Key != credsEnvKey {
		t.Fatalf("ResolveCreds lines = %+v", lines)
	}
	dec, err := base64.StdEncoding.DecodeString(lines[0].Value)
	if err != nil || string(dec) != blob {
		t.Errorf("blob did not round-trip: dec=%q err=%v", dec, err)
	}

	// A missing dotfile yields no line (claude runs unauthenticated).
	hc.Home = t.TempDir()
	if got := (Agent{}).ResolveCreds(hc); got != nil {
		t.Errorf("missing dotfile should yield no line, got %+v", got)
	}
}

func TestClaudeCredsHealth(t *testing.T) {
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour).UnixMilli()
	past := now.Add(-time.Hour).UnixMilli()
	tests := []struct {
		name    string
		blob    string
		wantOK  bool
		wantSub string // substring expected in reason when !wantOK
	}{
		{"empty", "", false, "empty"},
		{"whitespace only", "   \n", false, "empty"},
		{"healthy nested claudeAiOauth", fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, future), true, ""},
		{"healthy top-level fallback", fmt.Sprintf(`{"accessToken":"tok","expiresAt":%d}`, future), true, ""},
		{"healthy no expiry", `{"claudeAiOauth":{"accessToken":"tok"}}`, true, ""},
		{"expired token", fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, past), false, "expired"},
		{"no access token", `{"claudeAiOauth":{"expiresAt":12345}}`, false, "no accessToken"},
		{"unrecognised but valid json", `{"something":"else"}`, false, "no accessToken"},
		{"not json at all", "not-json-blob", true, ""}, // defer to in-container smoke test
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := claudeCredsHealth(tc.blob, now)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (reason=%q)", ok, tc.wantOK, reason)
			}
			if !tc.wantOK && tc.wantSub != "" && !strings.Contains(reason, tc.wantSub) {
				t.Errorf("reason = %q, want substring %q", reason, tc.wantSub)
			}
		})
	}
}
