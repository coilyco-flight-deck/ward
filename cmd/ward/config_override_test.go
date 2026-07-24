package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/fleetconfig"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// TestResolvedAgentKnobs pins the per-mode model/effort/endpoint projection the startup
// config echo reads (ward#616), so each agent surfaces the right resolved knobs.
func TestResolvedAgentKnobs(t *testing.T) {
	rc := agentsapi.RunCtx{
		ClaudeModel: "sonnet", ClaudeEffort: "medium",
		CodexModel: "gpt-5.4", CodexEffort: "low",
		GooseModel:    "qwen3-coder:30b",
		OpencodeModel: "qwen", OllamaURL: "http://x:1/v1",
	}
	cases := []struct {
		mode                    containerMode
		model, effort, endpoint string
	}{
		{modeClaude, "sonnet", "medium", ""},
		{modeCodex, "gpt-5.4", "low", ""},
		{modeOpencode, "qwen", "", "http://x:1/v1"},
		{modeGoose, "qwen3-coder:30b", "", "http://x:1/v1"},
	}
	for _, tc := range cases {
		m, e, ep := resolvedAgentKnobs(rc, tc.mode)
		if m != tc.model || e != tc.effort || ep != tc.endpoint {
			t.Errorf("%s: got (%q,%q,%q), want (%q,%q,%q)", tc.mode, m, e, ep, tc.model, tc.effort, tc.endpoint)
		}
	}
}

// TestParseConfigOverrides pins the `--config agent.<name>.<key>=<value>` dotted-path
// translation to the WARD_* container env keys (ward#616), including the loud failures.
func TestParseConfigOverrides(t *testing.T) {
	ok := []struct {
		name    string
		entries []string
		want    map[string]string
	}{
		{"nil is nil", nil, nil},
		{"claude model + effort", []string{"agent.claude.model=sonnet", "agent.claude.effort=medium"},
			map[string]string{"WARD_CLAUDE_MODEL": "sonnet", "WARD_CLAUDE_REASONING_EFFORT": "medium"}},
		{"goose model", []string{"agent.goose.model=qwen3-coder:30b"},
			map[string]string{"WARD_GOOSE_MODEL": "qwen3-coder:30b"}},
		{"codex verbosity", []string{"agent.codex.verbosity=high"},
			map[string]string{"WARD_CODEX_VERBOSITY": "high"}},
		{"opencode endpoint maps to ollama url", []string{"agent.opencode.endpoint=http://x:1/v1"},
			map[string]string{"WARD_OLLAMA_URL": "http://x:1/v1"}},
		{"blanks skipped, whitespace trimmed", []string{"  ", " agent.claude.model = opus "},
			map[string]string{"WARD_CLAUDE_MODEL": "opus"}},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConfigOverrides(tc.entries)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q = %q, want %q", k, got[k], v)
				}
			}
		})
	}

	bad := []struct {
		name  string
		entry string
	}{
		{"no equals", "agent.claude.model"},
		{"empty value", "agent.claude.model="},
		{"unknown agent", "agent.nope.model=x"},
		{"unknown key", "agent.claude.temperature=0.7"},
		{"missing agent prefix", "claude.model=x"},
	}
	for _, tc := range bad {
		t.Run("err/"+tc.name, func(t *testing.T) {
			if _, err := parseConfigOverrides([]string{tc.entry}); err == nil {
				t.Errorf("parseConfigOverrides(%q) = nil error, want a loud failure", tc.entry)
			}
		})
	}
}

// TestWardEnvMergesConfigOverrides pins that a resolved --config override rides into
// the container env as its WARD_* key, winning over a default (ward#616).
func TestWardEnvMergesConfigOverrides(t *testing.T) {
	p := upPlan{
		Name: "c",
		Repo: targetRepo{Owner: "o", Name: "r"},
		Mode: modeClaude,
		ConfigEnv: map[string]string{
			"WARD_CLAUDE_MODEL": "sonnet",
			"WARD_GOOSE_MODEL":  "qwen3-coder:30b",
		},
	}
	env := p.wardEnv()
	if got := env["WARD_CLAUDE_MODEL"]; got != "sonnet" {
		t.Errorf("wardEnv WARD_CLAUDE_MODEL = %q, want %q", got, "sonnet")
	}
	if got := env["WARD_GOOSE_MODEL"]; got != "qwen3-coder:30b" {
		t.Errorf("wardEnv WARD_GOOSE_MODEL = %q, want %q", got, "qwen3-coder:30b")
	}
}

func TestAddHarnessConfigEnvPrecedence(t *testing.T) {
	fleet := fleetconfig.Fleet{
		Agents: []fleetconfig.Agent{
			{Name: string(modeClaude), Model: "fleet-claude", ReasoningEffort: "medium"},
			{Name: string(modeCodex), Model: "fleet-codex", ReasoningEffort: "medium", Verbosity: "low"},
			{Name: string(modeOpencode), Model: "fleet-opencode", Endpoint: "http://fleet.example/v1"},
			{Name: string(modeGoose), Model: "fleet-goose"},
		},
		Roles: []fleetconfig.Role{{
			Name: roleEngineer,
			AgentConfig: map[string]fleetconfig.RoleAgentOverride{
				string(modeClaude):   {Model: "role-claude", ReasoningEffort: "high"},
				string(modeCodex):    {Model: "role-codex", ReasoningEffort: "high", Verbosity: "medium"},
				string(modeOpencode): {Model: "role-opencode", Endpoint: "http://role.example/v1"},
				string(modeGoose):    {Model: "role-goose"},
			},
		}},
	}
	t.Setenv("WARD_OPENCODE_MODEL", "env-opencode")
	t.Setenv("WARD_OLLAMA_URL", "")
	t.Setenv("WARD_GOOSE_MODEL", "")
	t.Setenv("WARD_CLAUDE_MODEL", "")
	t.Setenv("WARD_CLAUDE_REASONING_EFFORT", "")
	t.Setenv("WARD_CODEX_MODEL", "env-codex")
	t.Setenv("WARD_CODEX_REASONING_EFFORT", "")
	t.Setenv("WARD_CODEX_VERBOSITY", "")
	env := addHarnessConfigEnv(map[string]string{
		"WARD_GOOSE_MODEL":            "explicit-goose",
		"WARD_CODEX_REASONING_EFFORT": "explicit-effort",
	}, fleet, roleEngineer)
	if got := env["WARD_CLAUDE_MODEL"]; got != "role-claude" {
		t.Errorf("Claude role model = %q, want role-claude", got)
	}
	if got := env["WARD_CLAUDE_REASONING_EFFORT"]; got != "high" {
		t.Errorf("Claude role effort = %q, want high", got)
	}
	if got := env["WARD_CODEX_MODEL"]; got != "env-codex" {
		t.Errorf("Codex environment model = %q, want env-codex", got)
	}
	if got := env["WARD_CODEX_REASONING_EFFORT"]; got != "explicit-effort" {
		t.Errorf("Codex explicit effort = %q, want explicit-effort", got)
	}
	if got := env["WARD_CODEX_VERBOSITY"]; got != "medium" {
		t.Errorf("Codex role verbosity = %q, want medium", got)
	}
	if got := env["WARD_OPENCODE_MODEL"]; got != "env-opencode" {
		t.Errorf("environment model = %q, want env-opencode", got)
	}
	if got := env["WARD_OLLAMA_URL"]; got != "http://role.example/v1" {
		t.Errorf("role endpoint = %q, want role endpoint", got)
	}
	if got := env["WARD_GOOSE_MODEL"]; got != "explicit-goose" {
		t.Errorf("explicit Goose model = %q, want explicit-goose", got)
	}
}

func TestResolveLaunchConfigEnvIgnoresOperatorDirectorCodexOverlay(t *testing.T) {
	dir := writeBundleFixture(t)
	rolesPath := filepath.Join(dir, bundleFixtureRolesPath)
	body, err := os.ReadFile(rolesPath)
	if err != nil {
		t.Fatalf("read selected roles fixture: %v", err)
	}
	body = []byte(strings.Replace(string(body), "model gpt-5.5\n            reasoning-effort high", "model selected-director-model\n            reasoning-effort xhigh", 1))
	if err := os.WriteFile(rolesPath, body, 0o644); err != nil {
		t.Fatalf("write selected roles fixture: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	t.Setenv("WARD_CODEX_MODEL", "")
	t.Setenv("WARD_CODEX_REASONING_EFFORT", "")
	t.Setenv("WARD_CODEX_VERBOSITY", "")

	env, err := resolveLaunchConfigEnv(nil, "", roleDirector)
	if err != nil {
		t.Fatalf("resolveLaunchConfigEnv from baked policy: %v", err)
	}
	if got := env["WARD_CODEX_MODEL"]; got != "gpt-5.5" {
		t.Fatalf("projected Codex model = %q, want baked gpt-5.5", got)
	}
	if got := env["WARD_CODEX_REASONING_EFFORT"]; got != "high" {
		t.Fatalf("projected Codex effort = %q, want baked high", got)
	}

	for key, value := range env {
		t.Setenv(key, value)
	}
	t.Setenv("WARD_TARGET_OWNER", "owner")
	t.Setenv("WARD_TARGET_NAME", "repo")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.example")
	t.Setenv("WARD_MODE", string(modeCodex))
	t.Setenv("WARD_AGENT", string(modeCodex))
	t.Setenv("WARD_ROLE", roleDirector)
	bootstrap, err := readBootstrapEnv()
	if err != nil {
		t.Fatalf("readBootstrapEnv with baked policy: %v", err)
	}
	if bootstrap.CodexModel != "gpt-5.5" || bootstrap.CodexEffort != "high" {
		t.Fatalf("bootstrap Codex config = %q/%q, want baked gpt-5.5/high", bootstrap.CodexModel, bootstrap.CodexEffort)
	}
}

func TestValidateLocalHarnessConfig(t *testing.T) {
	for _, tc := range []struct {
		name            string
		mode            containerMode
		model, endpoint string
		wantErr         string
	}{
		{name: "cloud harness", mode: modeClaude},
		{name: "goose configured", mode: modeGoose, model: "local-model"},
		{name: "goose model missing", mode: modeGoose, wantErr: "agent.goose.model"},
		{name: "opencode configured", mode: modeOpencode, model: "local-model", endpoint: "http://local.example/v1"},
		{name: "opencode model missing", mode: modeOpencode, endpoint: "http://local.example/v1", wantErr: "agent.opencode.model"},
		{name: "opencode endpoint missing", mode: modeOpencode, model: "local-model", wantErr: "agent.opencode.endpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLocalHarnessConfig(tc.mode, tc.model, tc.endpoint)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v, want missing key %q", err, tc.wantErr)
			}
		})
	}
}

func TestAddFleetAttributionConfigEnv(t *testing.T) {
	fleet := fleetconfig.Fleet{
		Defaults: fleetconfig.Defaults{
			Attribution: fleetconfig.Attribution{Name: "coilyco-ops", Email: "coilyco-ops@coilysiren.me"},
		},
	}
	env := addFleetAttributionConfigEnv(map[string]string{"WARD_GIT_NAME": "manual-bot"}, fleet, "")
	if got := env["WARD_GIT_NAME"]; got != "manual-bot" {
		t.Errorf("WARD_GIT_NAME = %q, want manual-bot", got)
	}
	if got := env["WARD_GIT_EMAIL"]; got != "coilyco-ops@coilysiren.me" {
		t.Errorf("WARD_GIT_EMAIL = %q, want coilyco-ops@coilysiren.me", got)
	}
}

func TestAddFleetAttributionConfigEnvGitPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "repo user")
	runGit(t, repo, "config", "user.email", "repo@example.com")
	runGit(t, repo, "config", "--global", "user.name", "global user")
	runGit(t, repo, "config", "--global", "user.email", "global@example.com")

	fleet := fleetconfig.Fleet{
		Defaults: fleetconfig.Defaults{
			Attribution: fleetconfig.Attribution{Name: "example-bot", Email: "bot@example.com"},
		},
	}
	t.Run("explicit env wins", func(t *testing.T) {
		env := addFleetAttributionConfigEnv(map[string]string{
			"WARD_GIT_NAME":  "manual-bot",
			"WARD_GIT_EMAIL": "manual@example.com",
		}, fleet, repo)
		if got := env["WARD_GIT_NAME"]; got != "manual-bot" {
			t.Fatalf("WARD_GIT_NAME = %q, want manual-bot", got)
		}
		if got := env["WARD_GIT_EMAIL"]; got != "manual@example.com" {
			t.Fatalf("WARD_GIT_EMAIL = %q, want manual@example.com", got)
		}
	})
	t.Run("fleet attribution wins over git config", func(t *testing.T) {
		fleet := fleetconfig.Fleet{
			Defaults: fleetconfig.Defaults{
				Attribution: fleetconfig.Attribution{Name: "coilyco-ops", Email: "coilyco-ops@coilysiren.me"},
			},
		}
		env := addFleetAttributionConfigEnv(map[string]string{}, fleet, repo)
		if got := env["WARD_GIT_NAME"]; got != "coilyco-ops" {
			t.Fatalf("WARD_GIT_NAME = %q, want coilyco-ops", got)
		}
		if got := env["WARD_GIT_EMAIL"]; got != "coilyco-ops@coilysiren.me" {
			t.Fatalf("WARD_GIT_EMAIL = %q, want coilyco-ops@coilysiren.me", got)
		}
	})
	t.Run("git config fills placeholder fleet attribution", func(t *testing.T) {
		env := addFleetAttributionConfigEnv(map[string]string{}, fleet, repo)
		if got := env["WARD_GIT_NAME"]; got != "repo user" {
			t.Fatalf("WARD_GIT_NAME = %q, want repo user", got)
		}
		if got := env["WARD_GIT_EMAIL"]; got != "repo@example.com" {
			t.Fatalf("WARD_GIT_EMAIL = %q, want repo@example.com", got)
		}
	})
	t.Run("global config fills when local is absent", func(t *testing.T) {
		globalOnly := t.TempDir()
		runGit(t, globalOnly, "init")
		env := addFleetAttributionConfigEnv(map[string]string{}, fleet, globalOnly)
		if got := env["WARD_GIT_NAME"]; got != "global user" {
			t.Fatalf("WARD_GIT_NAME = %q, want global user", got)
		}
		if got := env["WARD_GIT_EMAIL"]; got != "global@example.com" {
			t.Fatalf("WARD_GIT_EMAIL = %q, want global@example.com", got)
		}
	})
}
