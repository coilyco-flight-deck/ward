package codex

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func noopLog(string, ...any) {}

// TestWriteCredsScrubsEnv: WriteCreds writes ~/.codex/auth.json then scrubs the
// bootstrap-only WARD_CODEX_AUTH_B64 env var (ward#357).
func TestWriteCredsScrubsEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv(authEnvKey, base64.StdEncoding.EncodeToString([]byte(`{"ok":1}`)))
	rc := agentsapi.RunCtx{AgentHome: home, Log: noopLog}
	if err := (Agent{}).WriteCreds(rc); err != nil {
		t.Fatalf("WriteCreds: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); err != nil {
		t.Fatalf("expected ~/.codex/auth.json written: %v", err)
	}
	if v := os.Getenv(authEnvKey); v != "" {
		t.Errorf("%s should be scrubbed after seeding, got %q", authEnvKey, v)
	}
}

// TestComposeConfigTrustDirs covers the codex workspace trust seed (ward#678): every
// rc.TrustDirs entry lands as a [projects] table with trust_level = "trusted".
func TestComposeConfigTrustDirs(t *testing.T) {
	home := t.TempDir()
	dirs := []string{"/workspace/ward", "/workspace", "/workspace/cli-guard"}
	rc := agentsapi.RunCtx{AgentHome: home, TargetName: "ward", TrustDirs: dirs, Log: noopLog}
	if err := (Agent{}).ComposeConfig(rc); err != nil {
		t.Fatalf("ComposeConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("expected ~/.codex/config.toml written: %v", err)
	}
	got := string(data)
	for _, dir := range dirs {
		want := "[projects.\"" + dir + "\"]\ntrust_level = \"trusted\"\n"
		if !strings.Contains(got, want) {
			t.Errorf("config.toml missing trust table for %s\n---\n%s", dir, got)
		}
	}
}

// TestComposeConfigTrustFallback: an empty TrustDirs still trusts the target
// clone, mirroring claude's onboarding fallback.
func TestComposeConfigTrustFallback(t *testing.T) {
	home := t.TempDir()
	rc := agentsapi.RunCtx{AgentHome: home, TargetName: "ward", Log: noopLog}
	if err := (Agent{}).ComposeConfig(rc); err != nil {
		t.Fatalf("ComposeConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("expected ~/.codex/config.toml written: %v", err)
	}
	if want := "[projects.\"/workspace/ward\"]\ntrust_level = \"trusted\"\n"; !strings.Contains(string(data), want) {
		t.Errorf("config.toml missing fallback trust table for the target clone\n---\n%s", data)
	}
}

// TestComposeConfigCheapDefaults guards the cheapest-by-default codex posture
// (ward#379): mini model + low reasoning/verbosity, overrides flow via RunCtx.
func TestComposeConfigCheapDefaults(t *testing.T) {
	home := t.TempDir()
	rc := agentsapi.RunCtx{AgentHome: home, Log: noopLog,
		CodexModel: "gpt-5.4-mini", CodexEffort: "low", CodexVerbosity: "low"}
	if err := (Agent{}).ComposeConfig(rc); err != nil {
		t.Fatalf("ComposeConfig: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("expected ~/.codex/config.toml written: %v", err)
	}
	for _, want := range []string{
		"approval_policy = \"never\"",
		"sandbox_mode = \"danger-full-access\"",
		"model = \"gpt-5.4-mini\"",
		"model_reasoning_effort = \"low\"",
		"model_verbosity = \"low\"",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("config.toml missing %q\n---\n%s", want, got)
		}
	}

	// Overrides flow straight through to the written config.
	rc2 := agentsapi.RunCtx{AgentHome: home, Log: noopLog,
		CodexModel: "gpt-5.5", CodexEffort: "high", CodexVerbosity: "medium"}
	if err := (Agent{}).ComposeConfig(rc2); err != nil {
		t.Fatalf("ComposeConfig override: %v", err)
	}
	got2, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	for _, want := range []string{"model = \"gpt-5.5\"", "model_reasoning_effort = \"high\"", "model_verbosity = \"medium\""} {
		if !strings.Contains(string(got2), want) {
			t.Errorf("overridden config.toml missing %q\n---\n%s", want, got2)
		}
	}
}
