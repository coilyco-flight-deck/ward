package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// gitRunner builds a Runner whose shell.Runner resolves git on PATH (stdio
// discarded); bare &Runner{} has a nil shell.Runner and would panic (ward#327).
func gitRunner() *Runner {
	return &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
}

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
			name:     "opencode oneshot",
			env:      bootstrapEnv{Mode: "opencode", Agent: "opencode", Headless: true},
			seed:     seed,
			wantArgv: []string{"opencode", "run", "work issue #5"},
		},
		{
			name:     "opencode interactive",
			env:      bootstrapEnv{Mode: "opencode", Agent: "opencode"},
			seed:     seed,
			wantArgv: []string{"opencode"},
		},
		{
			// The retired "qwen" alias (ward#401) still drives the opencode dialect.
			name:     "qwen alias oneshot",
			env:      bootstrapEnv{Mode: "qwen", Agent: "opencode", Headless: true},
			seed:     seed,
			wantArgv: []string{"opencode", "run", "work issue #5"},
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

func TestNamedGate(t *testing.T) {
	err := agentsapi.NewGateError("model-config", context.Canceled)
	if got, ok := namedGate(err); !ok || got != "model-config" {
		t.Fatalf("namedGate(%v) = %q,%v; want model-config,true", err, got, ok)
	}
	if got, ok := namedGate(context.Canceled); ok || got != "" {
		t.Fatalf("namedGate(non-gate) = %q,%v; want empty,false", got, ok)
	}
}

// TestReadBootstrapEnvDefaults checks the bash `${X:-default}` fallbacks and the
// required-var errors (`: "${X:?...}"`).
func TestReadBootstrapEnvDefaults(t *testing.T) {
	for _, k := range []string{
		"WARD_MODE", "WARD_AGENT", "WARD_CONTEXT_LEVEL", "WARD_GITCACHE", "WARD_CONTEXT_SRC",
		"WARD_QWEN_MODEL", "WARD_OLLAMA_URL", "WARD_GIT_NAME", "WARD_GIT_EMAIL",
		"WARD_CODEX_MODEL", "WARD_CODEX_REASONING_EFFORT", "WARD_CODEX_VERBOSITY",
		"WARD_AGENT_UID", "WARD_AGENT_GID", "WARD_AGENT_HOME", "WARD_BRANCH",
		"WARD_ROLE",
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
		"Mode":           e.Mode,
		"Agent":          e.Agent,
		"ContextLevel":   e.ContextLevel,
		"GitCache":       e.GitCache,
		"QwenModel":      e.QwenModel,
		"OllamaURL":      e.OllamaURL,
		"CodexModel":     e.CodexModel,
		"CodexEffort":    e.CodexEffort,
		"CodexVerbosity": e.CodexVerbosity,
		"GitUserName":    e.GitUserName,
		"GitUserEmail":   e.GitUserEmail,
		"AgentUID":       e.AgentUID,
		"AgentHome":      e.AgentHome,
		"ForgejoHost":    e.ForgejoHost,
	}
	want := map[string]string{
		"Mode":           "claude",
		"Agent":          "claude",
		"ContextLevel":   "2",
		"GitCache":       "/gitcache",
		"QwenModel":      "qwen3-coder:30b",
		"OllamaURL":      "http://localhost:11434/v1",
		"CodexModel":     "gpt-5.4",
		"CodexEffort":    "medium",
		"CodexVerbosity": "low",
		"GitUserName":    "coilyco-ops",
		"GitUserEmail":   "coilyco-ops@coilysiren.me",
		"AgentUID":       "1000",
		"AgentHome":      "/home/ubuntu",
		"ForgejoHost":    "forgejo.coilysiren.me",
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

// Claude onboarding + creds and codex creds/config drained to their agent
// folders in ward#425 Phase 3; their tests live there now (internal/agents/*).

// TestRevokeClonePushURL covers ward#327: a read-only session points origin's push
// URL at the dead no-push:// scheme while leaving the fetch URL intact.
func TestRevokeClonePushURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	work := t.TempDir()
	const origin = "https://forgejo.example/owner/repo.git"
	for _, argv := range [][]string{
		{"-C", work, "init", "-q"},
		{"-C", work, "remote", "add", "origin", origin},
	} {
		if out, err := exec.Command("git", argv...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", argv, err, out)
		}
	}

	gitRunner().revokeClonePushURL(context.Background(), work)

	push := gitURL(t, work, "--push")
	if push != noPushURL {
		t.Errorf("push URL = %q, want %q", push, noPushURL)
	}
	if fetch := gitURL(t, work, "--all"); !strings.Contains(fetch, origin) {
		t.Errorf("fetch URL %q lost the original %q; strip must leave fetch intact", fetch, origin)
	}
}

// gitURL reads origin's configured URL(s); flag selects --push or --all.
func gitURL(t *testing.T, work, flag string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", work, "remote", "get-url", flag, "origin").CombinedOutput()
	if err != nil {
		t.Fatalf("git remote get-url %s: %v\n%s", flag, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestInstallReadOnlyPushGuard covers ward#299: a read-only session lands the
// per-clone pre-push hook; a writable session and a missing .git/hooks do not.
func TestInstallReadOnlyPushGuard(t *testing.T) {
	r := gitRunner()

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

// opencode + goose config composers drained to their folders in ward#425
// Phase 3; TestConfigJSON / TestConfigYAML live there now.

// TestComposeContextRuntimeDoctrineLoadPoints covers ward#377 for Go bootstrap:
// canonical AGENTS.md feeds Codex, Claude, and Goose load points.
func TestComposeContextRuntimeDoctrineLoadPoints(t *testing.T) {
	const marker = "director's read-only surface session"
	r := &Runner{}

	home := t.TempDir()
	r.composeContext(bootstrapEnv{
		Mode:         "codex",
		ContextLevel: "0",
		ContextSrc:   filepath.Join(t.TempDir(), "absent"),
		AgentHome:    home,
		ReadOnly:     true,
	})
	for _, path := range []string{
		filepath.Join(home, "AGENTS.md"),
		filepath.Join(home, ".codex", "AGENTS.md"),
		filepath.Join(home, ".claude", "CLAUDE.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected runtime doctrine at %s: %v", path, err)
		}
		if !strings.Contains(string(data), marker) {
			t.Errorf("%s missing read-only director doctrine", path)
		}
	}
	if target, err := os.Readlink(filepath.Join(home, ".codex", "AGENTS.md")); err == nil && target != filepath.Join("..", "AGENTS.md") {
		t.Errorf("codex AGENTS.md link target = %q, want ../AGENTS.md", target)
	}

	gooseHome := t.TempDir()
	r.composeContext(bootstrapEnv{
		Mode:         "goose",
		ContextLevel: "0",
		ContextSrc:   filepath.Join(t.TempDir(), "absent"),
		AgentHome:    gooseHome,
		ReadOnly:     true,
	})
	ghints, err := os.ReadFile(filepath.Join(gooseHome, ".config", "goose", ".goosehints"))
	if err != nil {
		t.Fatalf("expected goose hints mirror: %v", err)
	}
	if !strings.Contains(string(ghints), marker) {
		t.Error("goose hints missing read-only director doctrine")
	}
}

func TestPrepareScratchSpace(t *testing.T) {
	r := &Runner{}
	scratch := t.TempDir()
	t.Setenv("TMPDIR", "")
	t.Setenv("TMP", "")
	t.Setenv("TEMP", "")
	r.prepareScratchSpace(scratch)
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got := os.Getenv(key); got != scratch {
			t.Errorf("%s = %q, want %s", key, got, scratch)
		}
	}
	if info, err := os.Stat(scratch); err != nil || !info.IsDir() {
		t.Fatalf("%s not provisioned: %v", scratch, err)
	}
}

func TestMakeReadOnlyTree(t *testing.T) {
	root := t.TempDir()
	defer func() {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				//nolint:nilerr // test cleanup skips transient walk errors
				return nil
			}
			mode := info.Mode().Perm()
			if info.IsDir() {
				mode |= 0o755
			} else {
				mode |= 0o644
			}
			_ = os.Chmod(path, mode)
			return nil
		})
	}()
	subdir := filepath.Join(root, "dir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(subdir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Runner{}
	r.makeReadOnlyTree(root)

	for _, path := range []string{root, subdir, file} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Errorf("%s still writable after makeReadOnlyTree: mode %o", path, info.Mode().Perm())
		}
	}
}

// The goose ollama-host scrub test drained to internal/agents/goose (ward#425).

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
