package main

import (
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// TestResolvedAgentKnobs pins the per-mode model/effort/endpoint projection the startup
// config echo reads (ward#616), so each agent surfaces the right resolved knobs.
func TestResolvedAgentKnobs(t *testing.T) {
	rc := agentsapi.RunCtx{
		ClaudeModel: "sonnet", ClaudeEffort: "medium",
		CodexModel: "gpt-5.4", CodexEffort: "low",
		OpencodeModel: "qwen", OllamaURL: "http://x:1/v1",
	}
	cases := []struct {
		mode                    containerMode
		model, effort, endpoint string
	}{
		{modeClaude, "sonnet", "medium", ""},
		{modeCodex, "gpt-5.4", "low", ""},
		{modeOpencode, "qwen", "", "http://x:1/v1"},
		{modeGoose, "", "", ""},
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
		Name:      "c",
		Repo:      targetRepo{Owner: "o", Name: "r"},
		Mode:      modeClaude,
		ConfigEnv: map[string]string{"WARD_CLAUDE_MODEL": "sonnet"},
	}
	env := p.wardEnv()
	if got := env["WARD_CLAUDE_MODEL"]; got != "sonnet" {
		t.Errorf("wardEnv WARD_CLAUDE_MODEL = %q, want %q", got, "sonnet")
	}
}
