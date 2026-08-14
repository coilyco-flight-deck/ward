package codex

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func noopLog(string, ...any) {}

type fakeExec struct {
	out  []byte
	err  error
	bin  string
	argv []string
}

func (f *fakeExec) Exec(context.Context, string, ...string) error { return nil }

func (f *fakeExec) Capture(_ context.Context, bin string, argv ...string) ([]byte, error) {
	f.bin = bin
	f.argv = append([]string(nil), argv...)
	return f.out, f.err
}

func decodedAuthLine(t *testing.T, lines []agentsapi.EnvLine) string {
	t.Helper()
	if len(lines) != 1 || lines[0].Key != authEnvKey {
		t.Fatalf("ResolveCreds lines = %+v", lines)
	}
	decoded, err := base64.StdEncoding.DecodeString(lines[0].Value)
	if err != nil {
		t.Fatalf("decode auth line: %v", err)
	}
	return string(decoded)
}

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

// TestResolveCredsPrefersFile preserves the cross-platform auth.json contract
// when a macOS Keychain credential is also available (ward#1641).
func TestResolveCredsPrefersFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const fileBlob = `{"source":"file"}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(fileBlob), 0o600); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{out: []byte(`{"source":"keychain"}`)}
	hc := agentsapi.HostCtx{Ctx: context.Background(), GOOS: "darwin", Home: home, Exec: exec, Log: noopLog}
	if got := decodedAuthLine(t, (Agent{}).ResolveCreds(hc)); got != fileBlob {
		t.Errorf("resolved auth = %q, want file credential %q", got, fileBlob)
	}
	if exec.bin != "" {
		t.Errorf("Keychain lookup ran despite a usable auth.json: %s %v", exec.bin, exec.argv)
	}
}

// TestResolveCredsFromMacOSKeychain covers the host-only Keychain fallback and
// exact security(1) lookup contract (ward#1641).
func TestResolveCredsFromMacOSKeychain(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const keychainBlob = `{"source":"keychain"}`
	exec := &fakeExec{out: []byte("  " + keychainBlob + "\n")}
	hc := agentsapi.HostCtx{Ctx: context.Background(), GOOS: "darwin", Home: home, Exec: exec, Log: noopLog}
	if got := decodedAuthLine(t, (Agent{}).ResolveCreds(hc)); got != keychainBlob {
		t.Errorf("resolved auth = %q, want Keychain credential %q", got, keychainBlob)
	}
	wantArgv := []string{"find-generic-password", "-s", codexKeychainService, "-a", codexKeychainAccount(filepath.Join(home, ".codex")), "-w"}
	if exec.bin != "security" || strings.Join(exec.argv, "\x00") != strings.Join(wantArgv, "\x00") {
		t.Errorf("Keychain lookup = %s %v, want security %v", exec.bin, exec.argv, wantArgv)
	}
}

func TestCodexKeychainAccount(t *testing.T) {
	if got, want := codexKeychainAccount("/Users/example/.codex"), "cli|8533f99d37c07dbb"; got != want {
		t.Errorf("codexKeychainAccount = %q, want %q", got, want)
	}

	realHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(realHome, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkRoot := t.TempDir()
	linkHome := filepath.Join(linkRoot, "home-link")
	if err := os.Symlink(realHome, linkHome); err != nil {
		t.Fatal(err)
	}
	if got, want := codexKeychainAccount(filepath.Join(linkHome, ".codex")), codexKeychainAccount(filepath.Join(realHome, ".codex")); got != want {
		t.Errorf("symlinked CODEX_HOME account = %q, canonical account %q", got, want)
	}
}

func TestResolveCredsKeychainFailureAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		exec *fakeExec
	}{
		{name: "lookup failure", exec: &fakeExec{err: errors.New("not found")}},
		{name: "empty item", exec: &fakeExec{out: []byte(" \n")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs []string
			hc := agentsapi.HostCtx{
				Ctx: context.Background(), GOOS: "darwin", Home: t.TempDir(), Exec: tc.exec,
				Log: func(format string, _ ...any) { logs = append(logs, format) },
			}
			if got := (Agent{}).ResolveCreds(hc); got != nil {
				t.Errorf("ResolveCreds = %+v, want nil", got)
			}
			if len(logs) != 1 {
				t.Errorf("log count = %d, want 1", len(logs))
			}
		})
	}
}

func TestResolveCredsNonMacDoesNotReadKeychain(t *testing.T) {
	exec := &fakeExec{out: []byte(`{"source":"keychain"}`)}
	hc := agentsapi.HostCtx{Ctx: context.Background(), GOOS: "linux", Home: t.TempDir(), Exec: exec, Log: noopLog}
	if got := (Agent{}).ResolveCreds(hc); got != nil {
		t.Errorf("ResolveCreds = %+v, want nil", got)
	}
	if exec.bin != "" {
		t.Errorf("non-macOS lookup ran Keychain command: %s %v", exec.bin, exec.argv)
	}
}

// TestComposeConfigTrustDirs covers the codex workspace trust seed (ward#678): every
// rc.TrustDirs entry lands as a [projects] table with trust_level = "trusted".
func TestComposeConfigTrustDirs(t *testing.T) {
	home := t.TempDir()
	dirs := []string{"/workspace/ward", "/workspace", "/workspace/umbra"}
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
		"notice.hide_rate_limit_model_nudge = true",
		"model = \"gpt-5.4-mini\"",
		"model_reasoning_effort = \"low\"",
		"model_verbosity = \"low\"",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("config.toml missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "[mcp_servers.") {
		t.Errorf("config.toml must leave Codex MCP servers unmanaged\n---\n%s", got)
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
