package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestStreamProgress asserts the stream-json -> concise-line port matches the
// bash jq filter for the event kinds it handles.
func TestStreamProgress(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hello\nthere"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/x.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls -la"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","is_error":true}]}}`,
		`{"type":"result","subtype":"success","num_turns":3,"duration_ms":4500,"result":"all done"}`,
		`not json at all`,
		``,
		`{"type":"system","subtype":"init"}`,
	}, "\n")

	var out strings.Builder
	streamProgress(strings.NewReader(in), &out)
	got := out.String()

	want := []string{
		"  hello there",
		"● Read /tmp/x.go",
		"● Bash ls -la",
		"  ✗ (tool error)",
		"✓ result: success (3 turns, 4s)",
		"all done",
	}
	gotLines := splitNonEmpty(got)
	if !slices.Equal(gotLines, want) {
		t.Errorf("streamProgress lines mismatch\n got: %#v\nwant: %#v", gotLines, want)
	}
}

// TestStreamProgressToolArgPrecedence checks the tool-arg coalesce order
// (file_path before command before path/pattern/url), like the jq `//` chain.
func TestStreamProgressToolArgPrecedence(t *testing.T) {
	in := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"foo","url":"http://x"}}]}}`
	var out strings.Builder
	streamProgress(strings.NewReader(in), &out)
	if got := strings.TrimSpace(out.String()); got != "● Grep foo" {
		t.Errorf("tool arg precedence: got %q, want %q", got, "● Grep foo")
	}
}

// TestStreamProgressTruncation caps text at 140 runes and tool args at 120.
func TestStreamProgressTruncation(t *testing.T) {
	longText := strings.Repeat("a", 200)
	in := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + longText + `"}]}}`
	var out strings.Builder
	streamProgress(strings.NewReader(in), &out)
	got := strings.TrimSpace(out.String())
	if got != strings.Repeat("a", 140) {
		t.Errorf("text not truncated to 140: len=%d", len([]rune(got)))
	}
}

// TestBuildAgentArgv covers the per-mode argv builder for every mode + run kind.
func TestBuildAgentArgv(t *testing.T) {
	seed := []string{"work issue #5"}
	cases := []struct {
		name       string
		env        bootstrapEnv
		seed       []string
		wantArgv   []string
		wantStream bool
	}{
		{
			name:     "claude interactive",
			env:      bootstrapEnv{Mode: "claude", Agent: "claude"},
			seed:     seed,
			wantArgv: []string{"claude", "work issue #5"},
		},
		{
			name:     "claude ask",
			env:      bootstrapEnv{Mode: "claude", Agent: "claude", Ask: true},
			seed:     seed,
			wantArgv: []string{"claude", "-p", "work issue #5"},
		},
		{
			name:       "claude headless",
			env:        bootstrapEnv{Mode: "claude", Agent: "claude", Headless: true},
			seed:       seed,
			wantArgv:   []string{"claude", "-p", "--verbose", "--output-format", "stream-json", "work issue #5"},
			wantStream: true,
		},
		{
			name:     "goose oneshot",
			env:      bootstrapEnv{Mode: "goose", Agent: "goose", Headless: true},
			seed:     seed,
			wantArgv: []string{"goose", "run", "-t", "work issue #5"},
		},
		{
			name:     "goose interactive",
			env:      bootstrapEnv{Mode: "goose", Agent: "goose"},
			seed:     seed,
			wantArgv: []string{"goose", "session"},
		},
		{
			name:     "codex oneshot",
			env:      bootstrapEnv{Mode: "codex", Agent: "codex", Ask: true},
			seed:     seed,
			wantArgv: []string{"codex", "exec", "work issue #5"},
		},
		{
			name:     "codex interactive",
			env:      bootstrapEnv{Mode: "codex", Agent: "codex"},
			seed:     seed,
			wantArgv: []string{"codex", "work issue #5"},
		},
		{
			name:     "qwen oneshot",
			env:      bootstrapEnv{Mode: "qwen", Agent: "opencode", Headless: true},
			seed:     seed,
			wantArgv: []string{"opencode", "run", "work issue #5"},
		},
		{
			name:     "qwen interactive",
			env:      bootstrapEnv{Mode: "qwen", Agent: "opencode"},
			seed:     seed,
			wantArgv: []string{"opencode"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv, stream := buildAgentArgv(tc.env, tc.seed)
			if !slices.Equal(argv, tc.wantArgv) {
				t.Errorf("argv = %#v, want %#v", argv, tc.wantArgv)
			}
			if stream != tc.wantStream {
				t.Errorf("stream = %v, want %v", stream, tc.wantStream)
			}
		})
	}
}

// TestReadBootstrapEnvDefaults checks the bash `${X:-default}` fallbacks and the
// required-var errors (`: "${X:?...}"`).
func TestReadBootstrapEnvDefaults(t *testing.T) {
	for _, k := range []string{
		"WARD_MODE", "WARD_AGENT", "WARD_CONTEXT_LEVEL", "WARD_GITCACHE", "WARD_CONTEXT_SRC",
		"WARD_QWEN_MODEL", "WARD_OLLAMA_URL", "WARD_GIT_NAME", "WARD_GIT_EMAIL",
		"WARD_AGENT_UID", "WARD_AGENT_GID", "WARD_AGENT_HOME", "WARD_BRANCH",
		"WARD_HEADLESS", "WARD_ASK", "WARD_MIRROR_NAME", "WARD_SUBSTRATE_SKIP",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("WARD_TARGET_OWNER", "coilysiren")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me/path")

	e, err := readBootstrapEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := map[string]string{
		"Mode":         e.Mode,
		"Agent":        e.Agent,
		"ContextLevel": e.ContextLevel,
		"GitCache":     e.GitCache,
		"QwenModel":    e.QwenModel,
		"OllamaURL":    e.OllamaURL,
		"GitUserName":  e.GitUserName,
		"GitUserEmail": e.GitUserEmail,
		"AgentUID":     e.AgentUID,
		"AgentHome":    e.AgentHome,
		"ForgejoHost":  e.ForgejoHost,
	}
	want := map[string]string{
		"Mode":         "claude",
		"Agent":        "claude",
		"ContextLevel": "2",
		"GitCache":     "/gitcache",
		"QwenModel":    "qwen2.5-coder:latest",
		"OllamaURL":    "http://localhost:11434/v1",
		"GitUserName":  "coilyco-ops",
		"GitUserEmail": "coilyco-ops@coilysiren.me",
		"AgentUID":     "1000",
		"AgentHome":    "/home/ubuntu",
		"ForgejoHost":  "forgejo.coilysiren.me",
	}
	for field, got := range checks {
		if got != want[field] {
			t.Errorf("%s = %q, want %q", field, got, want[field])
		}
	}
	if e.Headless || e.Ask {
		t.Errorf("Headless/Ask should default false: %v/%v", e.Headless, e.Ask)
	}
}

// TestSeedClaudeOnboarding covers ward#305: claude mode seeds ~/.claude.json so the
// interactive session skips the first-run theme picker; other modes write nothing.
func TestSeedClaudeOnboarding(t *testing.T) {
	r := &Runner{}

	t.Run("claude mode seeds onboarding", func(t *testing.T) {
		home := t.TempDir()
		r.seedClaudeOnboarding(bootstrapEnv{Mode: "claude", AgentHome: home, TargetName: "ward"})
		data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
		if err != nil {
			t.Fatalf("expected ~/.claude.json: %v", err)
		}
		// Decode so we assert the nested shape claude persists (ward#313): bypass-mode
		// acceptance at top level + folder trust under launch cwd /workspace/<target>.
		var got struct {
			HasCompletedOnboarding        bool `json:"hasCompletedOnboarding"`
			BypassPermissionsModeAccepted bool `json:"bypassPermissionsModeAccepted"`
			Projects                      map[string]struct {
				HasTrustDialogAccepted        bool `json:"hasTrustDialogAccepted"`
				HasCompletedProjectOnboarding bool `json:"hasCompletedProjectOnboarding"`
			} `json:"projects"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("claude.json is not valid JSON: %v\n%s", err, data)
		}
		if !got.HasCompletedOnboarding {
			t.Errorf("claude.json missing onboarding flag: %s", data)
		}
		if !got.BypassPermissionsModeAccepted {
			t.Errorf("claude.json missing bypassPermissionsModeAccepted: %s", data)
		}
		proj, ok := got.Projects["/workspace/ward"]
		if !ok {
			t.Fatalf("claude.json missing projects[/workspace/ward]: %s", data)
		}
		if !proj.HasTrustDialogAccepted || !proj.HasCompletedProjectOnboarding {
			t.Errorf("claude.json missing folder-trust flags for launch cwd: %s", data)
		}
	})

	t.Run("non-claude mode writes nothing", func(t *testing.T) {
		home := t.TempDir()
		r.seedClaudeOnboarding(bootstrapEnv{Mode: "codex", AgentHome: home})
		if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
			t.Errorf("codex mode should not write claude.json (err=%v)", err)
		}
	})
}

// TestInstallReadOnlyPushGuard covers ward#299: a read-only session lands the
// per-clone pre-push hook; a writable session and a missing .git/hooks do not.
func TestInstallReadOnlyPushGuard(t *testing.T) {
	r := &Runner{}

	t.Run("read-only session installs the executable hook", func(t *testing.T) {
		work := t.TempDir()
		if err := os.MkdirAll(filepath.Join(work, ".git", "hooks"), 0o755); err != nil {
			t.Fatalf("mkdir .git/hooks: %v", err)
		}
		r.installReadOnlyPushGuard(context.Background(), bootstrapEnv{ReadOnly: true}, work)
		hook := filepath.Join(work, ".git", "hooks", "pre-push")
		fi, err := os.Stat(hook)
		if err != nil {
			t.Fatalf("expected pre-push hook: %v", err)
		}
		if fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("pre-push hook is not executable: %v", fi.Mode())
		}
		body, err := os.ReadFile(hook)
		if err != nil {
			t.Fatalf("read hook: %v", err)
		}
		for _, want := range []string{"#!/bin/sh", "this clone can't push (ward#293, ward#315)", "exit 1"} {
			if !strings.Contains(string(body), want) {
				t.Errorf("hook missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("writable session installs nothing", func(t *testing.T) {
		work := t.TempDir()
		if err := os.MkdirAll(filepath.Join(work, ".git", "hooks"), 0o755); err != nil {
			t.Fatalf("mkdir .git/hooks: %v", err)
		}
		r.installReadOnlyPushGuard(context.Background(), bootstrapEnv{ReadOnly: false}, work)
		if _, err := os.Stat(filepath.Join(work, ".git", "hooks", "pre-push")); !os.IsNotExist(err) {
			t.Errorf("writable session should not write pre-push (err=%v)", err)
		}
	})

	t.Run("missing .git/hooks is tolerated", func(t *testing.T) {
		work := t.TempDir()
		r.installReadOnlyPushGuard(context.Background(), bootstrapEnv{ReadOnly: true}, work)
		if _, err := os.Stat(filepath.Join(work, ".git", "hooks", "pre-push")); !os.IsNotExist(err) {
			t.Errorf("no .git/hooks should be a no-op (err=%v)", err)
		}
	})
}

// TestParseExtraReposEnv covers the in-container WARD_EXTRA_REPOS parse (ward#230):
// whitespace-split, target + dup dropped, malformed entries skipped leniently.
func TestParseExtraReposEnv(t *testing.T) {
	got := parseExtraReposEnv(
		"coilyco-gaming/eco-protos coilysiren/ward coilyco-gaming/eco-protos garbage coilysiren/eco-app",
		"coilysiren", "eco-app")
	want := []targetRepo{
		{Owner: "coilyco-gaming", Name: "eco-protos"},
		{Owner: "coilysiren", Name: "ward"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d repos, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("repo[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if r := parseExtraReposEnv("", "o", "n"); r != nil {
		t.Errorf("empty WARD_EXTRA_REPOS should parse to nil, got %+v", r)
	}
}

// TestReadBootstrapEnvExtraRepos asserts readBootstrapEnv lifts WARD_EXTRA_REPOS
// into e.ExtraRepos, dropping the target (ward#230).
func TestReadBootstrapEnvExtraRepos(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilysiren")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")
	t.Setenv("WARD_EXTRA_REPOS", "coilyco-gaming/eco-protos coilysiren/ward")
	e, err := readBootstrapEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(e.ExtraRepos) != 1 || e.ExtraRepos[0].slug() != "coilyco-gaming/eco-protos" {
		t.Errorf("ExtraRepos = %+v, want only coilyco-gaming/eco-protos (target dropped)", e.ExtraRepos)
	}
}

// TestReadBootstrapEnvRequired asserts each missing required var errors.
func TestReadBootstrapEnvRequired(t *testing.T) {
	cases := []struct {
		clear string
		want  string
	}{
		{"WARD_TARGET_OWNER", "missing WARD_TARGET_OWNER"},
		{"WARD_TARGET_NAME", "missing WARD_TARGET_NAME"},
		{"WARD_FORGEJO_BASE", "missing WARD_FORGEJO_BASE"},
	}
	for _, tc := range cases {
		t.Run(tc.clear, func(t *testing.T) {
			t.Setenv("WARD_TARGET_OWNER", "o")
			t.Setenv("WARD_TARGET_NAME", "n")
			t.Setenv("WARD_FORGEJO_BASE", "https://x")
			t.Setenv(tc.clear, "")
			_, err := readBootstrapEnv()
			if err == nil || err.Error() != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestForgejoHostFromBase mirrors the bash sed host extraction.
func TestForgejoHostFromBase(t *testing.T) {
	cases := map[string]string{
		"https://forgejo.coilysiren.me":          "forgejo.coilysiren.me",
		"https://forgejo.coilysiren.me/":         "forgejo.coilysiren.me",
		"http://example.com/owner/name":          "example.com",
		"https://host.tld/a/b/c":                 "host.tld",
		"forgejo.coilysiren.me/already/no/proto": "forgejo.coilysiren.me",
	}
	for in, want := range cases {
		if got := forgejoHostFromBase(in); got != want {
			t.Errorf("forgejoHostFromBase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSplitOwnerName mirrors the bash `${ref%%/*}` / `${ref##*/}`.
func TestSplitOwnerName(t *testing.T) {
	cases := []struct {
		in          string
		owner, name string
		ok          bool
	}{
		{"coilysiren/ward", "coilysiren", "ward", true},
		{"a/b/c", "a", "c", true},
		{"noslash", "", "", false},
		{"/leading", "", "", false},
		{"trailing/", "", "", false},
	}
	for _, tc := range cases {
		owner, name, ok := splitOwnerName(tc.in)
		if owner != tc.owner || name != tc.name || ok != tc.ok {
			t.Errorf("splitOwnerName(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.in, owner, name, ok, tc.owner, tc.name, tc.ok)
		}
	}
}

// TestOpencodeConfigJSON keeps the literal $schema key (not interpolated) and
// interpolates the model + URL in the right places.
func TestOpencodeConfigJSON(t *testing.T) {
	got := opencodeConfigJSON("qwen2.5-coder:latest", "http://localhost:11434/v1")
	for _, want := range []string{
		`"$schema": "https://opencode.ai/config.json"`,
		`"model": "ollama/qwen2.5-coder:latest"`,
		`"baseURL": "http://localhost:11434/v1"`,
		`"qwen2.5-coder:latest": {}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("opencode config missing %q in:\n%s", want, got)
		}
	}
}

// TestGooseConfigYAML omits OLLAMA_HOST when no host resolved, includes it
// otherwise, matching the bash heredoc.
func TestGooseConfigYAML(t *testing.T) {
	noHost := gooseConfigYAML("ollama", "qwen2.5", "")
	if strings.Contains(noHost, "OLLAMA_HOST") {
		t.Errorf("no-host config should omit OLLAMA_HOST:\n%s", noHost)
	}
	if !strings.Contains(noHost, "GOOSE_PROVIDER: ollama") || !strings.Contains(noHost, "GOOSE_MODEL: qwen2.5") {
		t.Errorf("missing provider/model:\n%s", noHost)
	}
	withHost := gooseConfigYAML("ollama", "qwen2.5", "http://tower:11434")
	if !strings.Contains(withHost, "OLLAMA_HOST: http://tower:11434") {
		t.Errorf("with-host config should include OLLAMA_HOST:\n%s", withHost)
	}
}

// splitNonEmpty splits text into non-empty trimmed lines for assertions.
func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
