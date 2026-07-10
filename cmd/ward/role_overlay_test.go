package main

import (
	"slices"
	"testing"
)

// clearModelEnv zeroes every WARD_* model/effort knob so a test reads the fleet
// default + role overlay, not the ambient host env, then sets the required vars.
func clearModelEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"WARD_CLAUDE_MODEL", "WARD_CLAUDE_REASONING_EFFORT",
		"WARD_CODEX_MODEL", "WARD_CODEX_REASONING_EFFORT", "WARD_CODEX_VERBOSITY",
		"WARD_QWEN_MODEL", "WARD_OLLAMA_URL", "WARD_MODE", "WARD_AGENT",
		"WARD_HEADLESS", "WARD_ASK", "WARD_ROLE",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")
}

// TestRoleOverlayResolvesModelEffort pins the per-role x per-agent overlay (ward#620):
// it slots between WARD_* env and the flat default, so director diverges from engineer.
func TestRoleOverlayResolvesModelEffort(t *testing.T) {
	cases := []struct {
		role                                               string
		claudeModel, claudeEffort, codexModel, codexEffort string
	}{
		// director: strongest model at 1M context, high effort (heartbeat lane).
		{"director", "claude-opus-4-8[1m]", "high", "gpt-5.5", "high"},
		// engineer: cheaper/faster model at the same medium effort (parallel fan-out).
		{"engineer", "claude-fable-5", "medium", "gpt-5.4-mini", "medium"},
		// advisor mirrors the director overlay for both supported frontier agents.
		{"advisor", "claude-opus-4-8[1m]", "high", "gpt-5.5", "high"},
		// an unknown/empty role carries no overlay: the flat default stands.
		{"", "", "", "gpt-5.4", "medium"},
	}
	for _, tc := range cases {
		t.Run("role="+tc.role, func(t *testing.T) {
			clearModelEnv(t)
			t.Setenv("WARD_ROLE", tc.role)
			e, err := readBootstrapEnv()
			if err != nil {
				t.Fatalf("readBootstrapEnv: %v", err)
			}
			if e.ClaudeModel != tc.claudeModel || e.ClaudeEffort != tc.claudeEffort {
				t.Errorf("claude = (%q,%q), want (%q,%q)", e.ClaudeModel, e.ClaudeEffort, tc.claudeModel, tc.claudeEffort)
			}
			if e.CodexModel != tc.codexModel || e.CodexEffort != tc.codexEffort {
				t.Errorf("codex = (%q,%q), want (%q,%q)", e.CodexModel, e.CodexEffort, tc.codexModel, tc.codexEffort)
			}
		})
	}
}

// TestRoleOverlayEnvWins pins WARD_* env > role overlay (ward#620): an explicit
// WARD_CLAUDE_MODEL (the container form of --config) beats the director overlay.
func TestRoleOverlayEnvWins(t *testing.T) {
	clearModelEnv(t)
	t.Setenv("WARD_ROLE", "director")
	t.Setenv("WARD_CLAUDE_MODEL", "sonnet")
	t.Setenv("WARD_CODEX_REASONING_EFFORT", "low")
	e, err := readBootstrapEnv()
	if err != nil {
		t.Fatalf("readBootstrapEnv: %v", err)
	}
	if e.ClaudeModel != "sonnet" {
		t.Errorf("ClaudeModel = %q, want %q (env beats the director overlay)", e.ClaudeModel, "sonnet")
	}
	// the un-overridden knob still resolves the director overlay (effort high).
	if e.ClaudeEffort != "high" {
		t.Errorf("ClaudeEffort = %q, want %q (director overlay stands where env is unset)", e.ClaudeEffort, "high")
	}
	if e.CodexEffort != "low" {
		t.Errorf("CodexEffort = %q, want %q (env beats the director overlay)", e.CodexEffort, "low")
	}
	// codex model un-overridden still resolves the director overlay.
	if e.CodexModel != "gpt-5.5" {
		t.Errorf("CodexModel = %q, want %q (director overlay stands)", e.CodexModel, "gpt-5.5")
	}
}

// TestRoleOverlayBadConfigRefDoesNotAffectCoreDefaults pins the core boundary:
// a bad WARD_CONFIG_REF does not disturb the baked role overlay or env precedence.
func TestRoleOverlayBadConfigRefDoesNotAffectCoreDefaults(t *testing.T) {
	clearModelEnv(t)
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	t.Setenv("WARD_ROLE", "director")
	e, err := readBootstrapEnv()
	if err != nil {
		t.Fatalf("readBootstrapEnv: %v", err)
	}
	if e.ClaudeModel != "claude-opus-4-8[1m]" || e.ClaudeEffort != "high" {
		t.Fatalf("bad ref disturbed the baked director overlay for claude: %+v", e)
	}
	if e.CodexModel != "gpt-5.5" || e.CodexEffort != "high" {
		t.Fatalf("bad ref disturbed the baked director overlay for codex: %+v", e)
	}

	clearModelEnv(t)
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")
	t.Setenv("WARD_ROLE", "director")
	t.Setenv("WARD_CLAUDE_MODEL", "env-claude")
	t.Setenv("WARD_CODEX_REASONING_EFFORT", "env-low")
	e, err = readBootstrapEnv()
	if err != nil {
		t.Fatalf("readBootstrapEnv: %v", err)
	}
	if e.ClaudeModel != "env-claude" {
		t.Fatalf("env did not beat the baked overlay for claude: %+v", e)
	}
	if e.CodexEffort != "env-low" {
		t.Fatalf("env did not beat the baked overlay for codex: %+v", e)
	}
}

// TestRoleOverlayLaunchArgv pins launch-resolution end to end (ward#620): a director
// claude headless run appends `--model claude-opus-4-8[1m]`, engineer `claude-fable-5`.
func TestRoleOverlayLaunchArgv(t *testing.T) {
	cases := []struct {
		role, wantModel string
	}{
		{"director", "claude-opus-4-8[1m]"},
		{"engineer", "claude-fable-5"},
	}
	for _, tc := range cases {
		t.Run("role="+tc.role, func(t *testing.T) {
			clearModelEnv(t)
			t.Setenv("WARD_ROLE", tc.role)
			t.Setenv("WARD_MODE", "claude")
			t.Setenv("WARD_AGENT", "claude")
			t.Setenv("WARD_HEADLESS", "1")
			e, err := readBootstrapEnv()
			if err != nil {
				t.Fatalf("readBootstrapEnv: %v", err)
			}
			argv, stream := buildAgentArgv(e, []string{"do the thing"})
			want := []string{"claude", "-p", "--verbose", "--output-format", "stream-json", "--model", tc.wantModel, "do the thing"}
			if !slices.Equal(argv, want) {
				t.Errorf("argv = %#v, want %#v", argv, want)
			}
			if !stream {
				t.Errorf("stream = false, want true for a headless claude run")
			}
		})
	}
}

// TestWardEnvEmitsConfigRole pins the config role riding in as WARD_ROLE (ward#620):
// the capability role (director), not the `session` label the surface wears.
func TestWardEnvEmitsConfigRole(t *testing.T) {
	p := upPlan{
		Name:       "c",
		Repo:       targetRepo{Owner: "o", Name: "r"},
		Mode:       modeClaude,
		Role:       roleSession,
		ConfigRole: roleDirector,
	}
	if got := p.wardEnv()["WARD_ROLE"]; got != roleDirector {
		t.Errorf("wardEnv WARD_ROLE = %q, want %q (the config role, not the session label)", got, roleDirector)
	}
	// a bare plan with no config role leaves WARD_ROLE unset (today's behavior).
	bare := upPlan{Name: "c", Repo: targetRepo{Owner: "o", Name: "r"}, Mode: modeClaude}
	if _, ok := bare.wardEnv()["WARD_ROLE"]; ok {
		t.Errorf("wardEnv emitted WARD_ROLE for a role-less plan, want it absent")
	}
}
