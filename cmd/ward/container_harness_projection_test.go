package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func TestHarnessProjectionMatrix(t *testing.T) {
	cases := []struct {
		mode        containerMode
		source      string
		instruction string
		skills      string
		config      []string
		credential  []string
		permission  []string
		onboarding  []string
		state       []string
		ownership   []string
	}{
		{modeClaude, "CLAUDE.md", ".claude/CLAUDE.md", ".claude/skills", nil,
			[]string{".claude/.credentials.json"}, []string{".claude/settings.json"}, []string{".claude.json"},
			[]string{".claude", ".claude.json"}, []string{".claude", ".claude.json"}},
		{modeCodex, "AGENTS.md", ".codex/AGENTS.md", ".agents/skills", []string{".codex/config.toml"},
			[]string{".codex/auth.json"}, nil, nil, []string{".codex"}, []string{".codex", ".agents"}},
		{modeGoose, ".goosehints", ".config/goose/.goosehints", ".agents/skills", []string{".config/goose/config.yaml"},
			nil, nil, nil, []string{".config/goose"}, []string{".config/goose", ".agents"}},
		{modeOpencode, "AGENTS.md", ".config/opencode/AGENTS.md", ".agents/skills", []string{".config/opencode/opencode.json"},
			nil, nil, nil, []string{".config/opencode", ".opencode"}, []string{".config/opencode", ".agents", ".opencode"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			agent := lookupAgent(tc.mode)
			if err := validateHarnessProjection(agent); err != nil {
				t.Fatalf("validateHarnessProjection: %v", err)
			}
			p := agent.Record().Projection
			if !slices.Equal(p.InstructionSources, []string{tc.source}) || p.InstructionPath != tc.instruction || p.SkillsPath != tc.skills {
				t.Fatalf("instruction projection = %+v, want source=%q instruction=%q skills=%q", p, tc.source, tc.instruction, tc.skills)
			}
			if !slices.Equal(p.ConfigPaths, tc.config) || !slices.Equal(p.CredentialPaths, tc.credential) ||
				!slices.Equal(p.PermissionPaths, tc.permission) || !slices.Equal(p.OnboardingPaths, tc.onboarding) ||
				!slices.Equal(p.StatePaths, tc.state) || !slices.Equal(p.OwnershipPaths, tc.ownership) {
				t.Fatalf("harness surface drifted: %+v", p)
			}
		})
	}
}

func TestAgentOwnershipPathsSelectedAndExistingOnly(t *testing.T) {
	allForeign := []string{".claude", ".claude.json", ".codex", ".agents", ".config/goose", ".config/opencode", ".opencode"}
	for _, mode := range []containerMode{modeClaude, modeCodex, modeGoose, modeOpencode} {
		t.Run(string(mode), func(t *testing.T) {
			home := t.TempDir()
			work := filepath.Join(t.TempDir(), "work")
			if err := os.Mkdir(work, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, rel := range allForeign {
				path := filepath.Join(home, filepath.FromSlash(rel))
				if rel == ".claude.json" {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
						t.Fatal(err)
					}
				} else if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := agentOwnershipPaths(bootstrapEnv{Mode: string(mode), AgentHome: home}, work)
			want := []string{work}
			for _, rel := range lookupAgent(mode).Record().Projection.OwnershipPaths {
				want = append(want, filepath.Join(home, filepath.FromSlash(rel)))
			}
			if !slices.Equal(got, want) {
				t.Fatalf("ownership paths = %v, want selected existing paths %v", got, want)
			}
			if mode == modeCodex && slices.Contains(got, filepath.Join(home, ".claude.json")) {
				t.Fatal("Codex ownership includes foreign ~/.claude.json")
			}
			emptyHome := t.TempDir()
			if got := agentOwnershipPaths(bootstrapEnv{Mode: string(mode), AgentHome: emptyHome}, work); !slices.Equal(got, []string{work}) {
				t.Fatalf("ownership included missing selected paths: %v", got)
			}
		})
	}
}

func TestComposeContextMissingCompatibleSourceUsesDiagnostic(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "CLAUDE.md"), []byte("foreign claude source"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := (&Runner{}).composeContext(bootstrapEnv{
		Mode: string(modeCodex), ContextLevel: "1", ContextSrc: source, AgentHome: home,
	}); err != nil {
		t.Fatalf("composeContext: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("No compatible codex instruction source")) ||
		bytes.Contains(got, []byte("foreign claude source")) {
		t.Fatalf("missing-source projection did not diagnose without fallback:\n%s", got)
	}
}

type incompleteProjectionAgent struct {
	agentsapi.Agent
}

func (incompleteProjectionAgent) Name() string { return "incomplete" }

func (incompleteProjectionAgent) Record() agentsapi.Manifest {
	return agentsapi.Manifest{Name: "incomplete"}
}

func TestValidateHarnessProjectionRejectsIncompleteAdapter(t *testing.T) {
	if err := validateHarnessProjection(incompleteProjectionAgent{}); err == nil {
		t.Fatal("incomplete adapter projection should fail before launch")
	}
}

func TestHarnessBootstrapEnvironmentIsScrubbedForEveryMode(t *testing.T) {
	for _, mode := range []containerMode{modeClaude, modeCodex, modeGoose, modeOpencode} {
		t.Run(string(mode), func(t *testing.T) {
			for _, key := range harnessBootstrapEnvKeys {
				t.Setenv(key, "foreign-or-consumed-credential")
			}
			scrubHarnessBootstrapEnv()
			for _, key := range harnessBootstrapEnvKeys {
				if value, ok := os.LookupEnv(key); ok {
					t.Errorf("%s child environment retained %s=%q", mode, key, value)
				}
			}
		})
	}
}

func TestInheritedHarnessCredentialIsSelectedOnly(t *testing.T) {
	values := map[string]string{
		claudeCredsEnvKey:     "claude-credential",
		codexAuthEnvKey:       "codex-credential",
		gooseOllamaHostEnvKey: "goose-endpoint",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	cases := []struct {
		mode containerMode
		key  string
	}{
		{modeClaude, claudeCredsEnvKey},
		{modeCodex, codexAuthEnvKey},
		{modeGoose, gooseOllamaHostEnvKey},
		{modeOpencode, ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			got := inheritedAgentCredential(tc.mode)
			if tc.key == "" {
				if len(got) != 0 {
					t.Fatalf("%s inherited foreign credential: %+v", tc.mode, got)
				}
				return
			}
			if len(got) != 1 || got[0].Key != tc.key || got[0].Value != values[tc.key] {
				t.Fatalf("%s inherited credentials = %+v, want only %s", tc.mode, got, tc.key)
			}
		})
	}
}

func TestComposeAgentContainerHarnessPureOutputs(t *testing.T) {
	allOutputs := []string{
		".claude/settings.json",
		".claude/.credentials.json",
		".claude.json",
		".codex/config.toml",
		".codex/auth.json",
		".config/goose/config.yaml",
		".config/opencode/opencode.json",
	}
	cases := []struct {
		mode containerMode
		want []string
	}{
		{modeClaude, allOutputs[:3]},
		{modeCodex, allOutputs[3:5]},
		{modeGoose, allOutputs[5:6]},
		{modeOpencode, allOutputs[6:]},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Setenv(claudeCredsEnvKey, base64.StdEncoding.EncodeToString([]byte(`{"claudeAiOauth":{"accessToken":"fixture"}}`)))
			t.Setenv(codexAuthEnvKey, base64.StdEncoding.EncodeToString([]byte(`{"tokens":{"access_token":"fixture"}}`)))
			t.Setenv(gooseOllamaHostEnvKey, base64.StdEncoding.EncodeToString([]byte("http://example.invalid/v1")))
			home := t.TempDir()
			r := testRunner()
			rc := r.agentRunCtx(context.Background(), bootstrapEnv{
				Mode: string(tc.mode), AgentHome: home, TargetName: "fixture",
				GooseModel: "fixture-model", OpencodeModel: "fixture-model",
				OllamaURL: "http://example.invalid/v1",
			}, nil)
			composeAgentContainer(lookupAgent(tc.mode), rc)
			for _, rel := range allOutputs {
				_, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel)))
				if slices.Contains(tc.want, rel) && err != nil {
					t.Errorf("%s missing selected output %s: %v", tc.mode, rel, err)
				}
				if !slices.Contains(tc.want, rel) && !os.IsNotExist(err) {
					t.Errorf("%s created foreign output %s: %v", tc.mode, rel, err)
				}
			}
		})
	}
}

func TestWriteWardInstructionRejectsForeignAndReplacesWardOwned(t *testing.T) {
	home := t.TempDir()
	dest := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeWardInstruction(home, dest, []byte("first")); err == nil {
		t.Fatal("foreign instruction replacement should fail closed")
	}
	if err := os.WriteFile(dest, []byte(wardInstructionMarker+"\n\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeWardInstruction(home, dest, []byte("replacement")); err != nil {
		t.Fatalf("replace Ward-owned instruction: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != wardInstructionMarker+"\n\nreplacement\n" {
		t.Fatalf("Ward-owned replacement = %q", got)
	}
}
