package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// gitRunner builds a Runner whose shell.Runner resolves git on PATH (stdio
// discarded); bare &Runner{} has a nil shell.Runner and would panic (ward#327).
func gitRunner() *Runner {
	return &Runner{Runner: &shell.Runner{Stdout: io.Discard, Stderr: io.Discard}}
}

func TestProjectNativeMCP(t *testing.T) {
	dir := t.TempDir()
	inventory := filepath.Join(dir, "mcporter.json")
	if err := os.WriteFile(inventory, []byte(`{"imports":[],"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("write inventory: %v", err)
	}
	argsPath := filepath.Join(dir, "args")
	t.Setenv("WARD_TEST_ARGS", argsPath)
	projector := filepath.Join(dir, "agent-compose")
	if err := os.WriteFile(projector, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WARD_TEST_ARGS\"\n"), 0o700); err != nil {
		t.Fatalf("write projector fixture: %v", err)
	}
	r := &Runner{Runner: &shell.Runner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Resolve: func(bin string) (string, error) {
			if bin != "agent-compose" {
				t.Fatalf("resolved binary = %q, want agent-compose", bin)
			}
			return projector, nil
		},
	}}
	home := filepath.Join(dir, "agent-home")
	if err := r.projectNativeMCP(t.Context(), inventory, home); err != nil {
		t.Fatalf("projectNativeMCP: %v", err)
	}
	got, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read projector argv: %v", err)
	}
	want := "mcp\n--inventory\n" + inventory + "\n--home\n" + home + "\n"
	if string(got) != want {
		t.Fatalf("projector argv = %q, want %q", got, want)
	}
}

func TestProjectNativeMCPSkipsMissingInventory(t *testing.T) {
	r := &Runner{Runner: &shell.Runner{Resolve: func(bin string) (string, error) {
		t.Fatalf("unexpected binary resolution for %q", bin)
		return "", nil
	}}}
	if err := r.projectNativeMCP(t.Context(), filepath.Join(t.TempDir(), "missing.json"), t.TempDir()); err != nil {
		t.Fatalf("missing inventory must be optional: %v", err)
	}
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
			wantArgv: []string{"goose", "run", "--no-session", "-t"},
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
			seed:     []string{"----- launch pre-flight -----"},
			wantArgv: []string{"codex", "exec", "--", "----- launch pre-flight -----"},
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

func TestLaunchAgentReportsOOMKilled(t *testing.T) {
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	setpriv := filepath.Join(dir, "setpriv")
	dockerBody := "#!/bin/sh\n" +
		"if [ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .State}}' ]; then\n" +
		"  printf '%s' " + shellQuote(`{"OOMKilled":true,"ExitCode":0}`) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	setprivBody := "#!/bin/sh\nkill -9 $$\n"
	if err := os.WriteFile(docker, []byte(dockerBody), 0o700); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write fake docker: %v", err)
	}
	if err := os.WriteFile(setpriv, []byte(setprivBody), 0o700); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write fake setpriv: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: shell.PathResolver}}
	var launchErr error
	stderr := captureTestStderr(t, func() {
		launchErr = r.launchAgent(context.Background(), bootstrapEnv{Container: "director-codex-vg94"}, t.TempDir(), []string{"codex"}, false, nil)
	})
	if launchErr == nil {
		t.Fatal("launchAgent swallowed the harness failure")
	}
	if !strings.Contains(stderr, "signal: killed") {
		t.Fatalf("launchAgent stderr missing signal kill breadcrumb:\n%s", stderr)
	}
	if !strings.Contains(stderr, "OOMKilled=true") {
		t.Fatalf("launchAgent stderr missing OOM breadcrumb:\n%s", stderr)
	}
}

func TestSetprivPrefixDropsRawForgejoTokenOnlyForReadOnlyDirector(t *testing.T) {
	readOnly := setprivPrefix(bootstrapEnv{AgentUID: "1000", AgentGID: "1000", AgentHome: "/home/agent", ReadOnly: true})
	joined := strings.Join(readOnly, " ")
	if !strings.Contains(joined, "env -u FORGEJO_TOKEN HOME=/home/agent") {
		t.Fatalf("read-only prefix = %q, want env to remove FORGEJO_TOKEN", joined)
	}
	normal := strings.Join(setprivPrefix(bootstrapEnv{AgentUID: "1000", AgentGID: "1000", AgentHome: "/home/agent"}), " ")
	if strings.Contains(normal, "FORGEJO_TOKEN") {
		t.Errorf("non-read-only prefix unexpectedly filters credential: %q", normal)
	}
}

func TestReadOnlyBootstrapStartsBrokerAndExportsOnlySocketCapability(t *testing.T) {
	socket := shortBrokerSocket(t)
	logPath := filepath.Join(t.TempDir(), "logs", "broker.log")
	t.Setenv(envBrokerSocket, socket)
	t.Setenv("WARD_BROKER_LOG", logPath)
	t.Setenv("FORGEJO_TOKEN", "token-a")
	orig := credentialBrokerCommand
	credentialBrokerCommand = func(ctx context.Context, _ string, socket, _ string) *exec.Cmd {
		ln, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatalf("test broker listener: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return exec.CommandContext(ctx, "sleep", "60")
	}
	t.Cleanup(func() { credentialBrokerCommand = orig })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := (&Runner{}).startCredentialBroker(ctx, bootstrapEnv{ReadOnly: true, AgentGID: strconv.Itoa(os.Getgid())}); err != nil {
		t.Fatalf("startCredentialBroker: %v", err)
	}
	if !isSocket(socket) {
		t.Fatalf("broker socket %s was not reachable", socket)
	}
	if got := os.Getenv(envBrokerSocket); got != socket {
		t.Errorf("%s = %q, want %q", envBrokerSocket, got, socket)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("broker log was not isolated at %s: %v", logPath, err)
	}
	if strings.Contains(strings.Join(setprivPrefix(bootstrapEnv{ReadOnly: true, AgentUID: "1000", AgentGID: "1000", AgentHome: "/home/agent"}), " "), "token-a") {
		t.Fatal("dropped agent launch argv contains the raw broker token")
	}
}

func TestGooseCompletionOutput(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"Issue #0 IMPLEMENTATION COMPLETE", true},
		{"All requirements satisfied and implementation complete", true},
		{"still working", false},
		{"issue closed", false},
	}
	for _, tc := range cases {
		if got := gooseCompletionOutput(tc.line); got != tc.want {
			t.Errorf("gooseCompletionOutput(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestRunContainerBootstrapGooseNoSessionExitsAndReaps(t *testing.T) {
	preserveScratchEnv(t)
	prevWorkspaceRoot := workspaceRoot
	workspaceRoot = t.TempDir()
	t.Cleanup(func() { workspaceRoot = prevWorkspaceRoot })
	t.Cleanup(func() { restoreWritableTree(t, workspaceRoot) })
	prevScratchMnt := surfaceScratchMnt
	surfaceScratchMnt = filepath.Join(t.TempDir(), "scratch")
	t.Cleanup(func() { surfaceScratchMnt = prevScratchMnt })

	base := t.TempDir()
	seed := t.TempDir()
	repo := filepath.Join(base, "coilyco-flight-deck", "goose-test.git")
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, base, "init", "--bare", repo)
	runGit(t, seed, "init", "-b", "main")
	runGit(t, seed, "config", "user.email", "test@example.com")
	runGit(t, seed, "config", "user.name", "Test User")
	runGitCommitAt(t, seed, "2026-07-10T09:00:00Z", "README.md", "seed\n", "seed")
	runGit(t, seed, "remote", "add", "origin", repo)
	runGit(t, seed, "push", "-u", "origin", "main")

	binDir := t.TempDir()
	promptPath := filepath.Join(t.TempDir(), "prompt")
	goose := filepath.Join(binDir, "goose")
	setpriv := filepath.Join(binDir, "setpriv")
	chown := filepath.Join(binDir, "chown")
	aws := filepath.Join(binDir, "aws")
	gooseBody := "#!/bin/sh\n" +
		"test \"$#\" -eq 3 || exit 2\n" +
		"IFS= read -r prompt\n" +
		"printf '%s' \"$prompt\" >\"$WARD_TEST_PROMPT_PATH\"\n" +
		"trap '' INT TERM HUP\n" +
		"printf '%s\\n' 'Issue #0 IMPLEMENTATION COMPLETE'\n" +
		"printf '%s\\n' 'All requirements satisfied and implementation complete'\n" +
		"while :; do :; done\n"
	setprivBody := "#!/bin/sh\nshift 4\nexec env \"$@\"\n"
	chownBody := "#!/bin/sh\nexit 0\n"
	awsBody := "#!/bin/sh\nprintf '%s\\n' http://127.0.0.1:11434\n"
	for path, body := range map[string]string{
		goose:   gooseBody,
		setpriv: setprivBody,
		chown:   chownBody,
		aws:     awsBody,
	} {
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil { // #nosec G306 -- test fixture
			t.Fatalf("write %s: %v", filepath.Base(path), err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FORGEJO_TOKEN", "")
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "goose-test")
	t.Setenv("WARD_FORGEJO_BASE", base)
	t.Setenv("WARD_CLONE_BASE", base)
	t.Setenv("WARD_GITCACHE", t.TempDir())
	t.Setenv("WARD_MIRROR_NAME", "goose-test-mirror")
	t.Setenv("WARD_AGENT_HOME", t.TempDir())
	t.Setenv("WARD_TARGET_ISSUE", "0")
	t.Setenv("WARD_CONTAINER_NAME", "engineer-goose-goose-test-990")
	t.Setenv("WARD_MODE", "goose")
	t.Setenv("WARD_AGENT", "goose")
	t.Setenv("WARD_GOOSE_MODEL", "configured-model")
	t.Setenv("WARD_HEADLESS", "1")
	t.Setenv("WARD_READONLY", "1")
	t.Setenv("WARD_SMOKE_TEST_SKIP", "1")
	t.Setenv("WARD_SUBSTRATE_SKIP", "1")
	t.Setenv("WARD_REAP_WORK", "1")
	t.Setenv("WARD_TEST_PROMPT_PATH", promptPath)
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig"))

	var stdout bytes.Buffer
	var runStderr bytes.Buffer
	r := &Runner{Runner: &shell.Runner{Stdout: &stdout, Stderr: &runStderr, Resolve: shell.PathResolver}}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const prompt = "----- launch pre-flight -----"
	cmd := parseCommandForTest(t, nil, []string{"bootstrap", "--", prompt})
	var runErr error

	stderr := captureTestStderr(t, func() {
		runErr = r.runContainerBootstrap(ctx, cmd)
	})
	if runErr != nil {
		t.Fatalf("runContainerBootstrap: %v\nshell stderr:\n%s\nward stderr:\n%s", runErr, runStderr.String(), stderr)
	}
	if got := stdout.String(); !strings.Contains(got, "Issue #0 IMPLEMENTATION COMPLETE") {
		t.Fatalf("stdout missing final Goose completion output:\n%s", got)
	}
	if got, err := os.ReadFile(promptPath); err != nil || string(got) != prompt {
		t.Fatalf("Goose stdin prompt = %q, %v; want %q", got, err, prompt)
	}
	if os.Getenv(envAgentLaunched) != "1" {
		t.Fatal("bootstrap did not mark the harness launch handoff")
	}
	for _, want := range []string{
		"goose terminal completion output observed; requesting exit",
		"goose process exited after terminal completion output",
		"reaping: salvage residual work before teardown",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func preserveScratchEnv(t *testing.T) {
	t.Helper()
	keys := []string{"TMPDIR", "TMP", "TEMP", "GOCACHE", "GOMODCACHE", "GOTMPDIR", "XDG_CACHE_HOME"}
	saved := make(map[string]*string, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			v := value
			saved[key] = &v
			continue
		}
		saved[key] = nil
	}
	t.Cleanup(func() {
		for _, key := range keys {
			if value := saved[key]; value != nil {
				_ = os.Setenv(key, *value)
				continue
			}
			_ = os.Unsetenv(key)
		}
	})
}

func restoreWritableTree(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			_ = os.Chmod(path, 0o755)
			return nil
		}
		_ = os.Chmod(path, 0o644)
		return nil
	})
}

func TestEnsureLaunchBinaryAvailable(t *testing.T) {
	if err := ensureLaunchBinaryAvailable("definitely-not-a-ward-binary"); err == nil {
		t.Fatal("missing agent binary should be a hard bootstrap failure")
	} else if !strings.Contains(err.Error(), "definitely-not-a-ward-binary") {
		t.Fatalf("error should name the missing binary, got %v", err)
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
		"WARD_OPENCODE_MODEL", "WARD_GOOSE_MODEL", "WARD_OLLAMA_URL", "WARD_GIT_NAME", "WARD_GIT_EMAIL",
		"WARD_CODEX_MODEL", "WARD_CODEX_REASONING_EFFORT", "WARD_CODEX_VERBOSITY",
		"WARD_AGENT_UID", "WARD_AGENT_GID", "WARD_AGENT_HOME", "WARD_BRANCH",
		"WARD_ROLE", "WARD_TS_SOCKS5",
		"WARD_HEADLESS", "WARD_ASK", "WARD_MIRROR_NAME", "WARD_SUBSTRATE_SKIP",
		"WARD_VERSION",
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
		"OpencodeModel":  e.OpencodeModel,
		"GooseModel":     e.GooseModel,
		"OllamaURL":      e.OllamaURL,
		"CodexModel":     e.CodexModel,
		"CodexEffort":    e.CodexEffort,
		"CodexVerbosity": e.CodexVerbosity,
		"GitUserName":    e.GitUserName,
		"GitUserEmail":   e.GitUserEmail,
		"AgentUID":       e.AgentUID,
		"AgentHome":      e.AgentHome,
		"ForgejoHost":    e.ForgejoHost,
		"WardVersion":    e.WardVersion,
	}
	want := map[string]string{
		"Mode":           "claude",
		"Agent":          "claude",
		"ContextLevel":   "2",
		"GitCache":       "/gitcache",
		"OpencodeModel":  "",
		"GooseModel":     "",
		"OllamaURL":      "",
		"CodexModel":     "gpt-5.4",
		"CodexEffort":    "medium",
		"CodexVerbosity": "low",
		"GitUserName":    "example-bot",
		"GitUserEmail":   "bot@example.com",
		"AgentUID":       "1000",
		"AgentHome":      "/home/ubuntu/.ward",
		"ForgejoHost":    "forgejo.coilysiren.me",
		"WardVersion":    "",
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

// TestReadBootstrapEnvDirectorCodexOverlay covers the director bootstrap path:
// the baked fleet overlay resolves high effort and the startup echo prints it.
func TestReadBootstrapEnvDirectorCodexOverlay(t *testing.T) {
	for _, k := range []string{
		"WARD_MODE", "WARD_AGENT", "WARD_CONTEXT_LEVEL", "WARD_GITCACHE", "WARD_CONTEXT_SRC",
		"WARD_OPENCODE_MODEL", "WARD_OLLAMA_URL", "WARD_GIT_NAME", "WARD_GIT_EMAIL",
		"WARD_CODEX_MODEL", "WARD_CODEX_REASONING_EFFORT", "WARD_CODEX_VERBOSITY",
		"WARD_AGENT_UID", "WARD_AGENT_GID", "WARD_AGENT_HOME", "WARD_BRANCH",
		"WARD_HEADLESS", "WARD_ASK", "WARD_MIRROR_NAME", "WARD_SUBSTRATE_SKIP",
		"WARD_ROLE",
		"WARD_VERSION",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.coilysiren.me")
	t.Setenv("WARD_MODE", "codex")
	t.Setenv("WARD_AGENT", "codex")
	t.Setenv("WARD_ROLE", "director")

	e, err := readBootstrapEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.CodexEffort != "high" {
		t.Fatalf("director overlay resolved codex effort = %q, want high", e.CodexEffort)
	}

	prev := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = prev })

	done := make(chan string, 1)
	go func() {
		var out strings.Builder
		_, _ = io.Copy(&out, r)
		done <- out.String()
	}()

	rc := (&Runner{}).agentRunCtx(t.Context(), e, nil)
	echoAgentConfigGo(e, rc, modeCodex)
	_ = w.Close()
	got := <-done
	if !strings.Contains(got, "agent:         codex") || !strings.Contains(got, "effort:        high") {
		t.Fatalf("director startup config echo should surface high codex effort; got:\n%s", got)
	}
}

// echoRunContextGo should name whether the ward version came from a host ward
// default, an explicit pin, or latest resolution so startup logs are actionable.
func TestEchoRunContextGoVersionSource(t *testing.T) {
	prev := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = prev })

	done := make(chan string, 1)
	go func() {
		var out strings.Builder
		_, _ = io.Copy(&out, r)
		done <- out.String()
	}()

	echoRunContextGo(bootstrapEnv{
		TargetOwner:       "coilyco-flight-deck",
		TargetName:        "ward",
		Container:         "ward-container",
		Mode:              "claude",
		Agent:             "claude",
		WardVersionSource: wardVersionLaunchLabel("v0.16.0", wardVersionSourceExplicit),
	}, []string{"task"})
	_ = w.Close()
	got := <-done
	if !strings.Contains(got, "ward:     explicit pin v0.16.0") {
		t.Fatalf("explicit pin should be visible in run context; got:\n%s", got)
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

func gitText(t *testing.T, dir string, argv ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, argv...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", argv, err, out)
	}
	return strings.TrimSpace(string(out))
}

func seedBranchResumeRepo(t *testing.T, cloneBase, name string) (string, string) {
	t.Helper()
	const owner = "owner"
	const branch = "issue-735"
	seed := t.TempDir()
	runGit(t, seed, "init", "-b", "main", "-q")
	runGit(t, seed, "config", "user.name", "Ward Test")
	runGit(t, seed, "config", "user.email", "ward@example.com")
	runGitCommitAt(t, seed, "2026-07-09T00:00:00Z", "main.txt", "main\n", "main commit")
	mainRev := mustGitRev(t, seed, "HEAD")
	branchRev := mainRev
	if branch != "" {
		runGit(t, seed, "checkout", "-b", branch)
		runGitCommitAt(t, seed, "2026-07-09T00:01:00Z", "branch.txt", branch+"\n", "branch commit")
		branchRev = mustGitRev(t, seed, "HEAD")
		runGit(t, seed, "checkout", "main")
	}
	remote := filepath.Join(cloneBase, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(remote), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, t.TempDir(), "clone", "--bare", seed, remote)
	return mainRev, branchRev
}

func useTestWorkspaceRoot(t *testing.T) string {
	t.Helper()
	old := workspaceRoot
	root := t.TempDir()
	workspaceRoot = root
	t.Cleanup(func() { workspaceRoot = old })
	return root
}

func TestCloneTargetResumesExistingOriginBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r := gitRunner()
	workspace := useTestWorkspaceRoot(t)
	cloneBase := t.TempDir()
	gitCache := t.TempDir()
	name := "target-branch-resume"
	branch := "issue-735"
	mainRev, branchRev := seedBranchResumeRepo(t, cloneBase, name)

	work, err := r.cloneTarget(context.Background(), bootstrapEnv{
		TargetOwner: "owner",
		TargetName:  name,
		ForgejoBase: "https://forgejo.example",
		CloneBase:   cloneBase,
		GitCache:    gitCache,
		MirrorName:  "mirror.git",
		Branch:      branch,
	})
	if err != nil {
		t.Fatalf("cloneTarget: %v", err)
	}
	if got := mustGitRev(t, work, "HEAD"); got != branchRev {
		t.Fatalf("HEAD = %s, want resumed branch rev %s", got, branchRev)
	}
	if got := gitText(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
		t.Fatalf("current branch = %q, want %q", got, branch)
	}
	if got := mustGitRev(t, work, "origin/"+branch); got != branchRev {
		t.Fatalf("origin/%s = %s, want %s", branch, got, branchRev)
	}
	if got := mustGitRev(t, work, "origin/main"); got != mainRev {
		t.Fatalf("origin/main = %s, want %s", got, mainRev)
	}
	if got := work; got != filepath.Join(workspace, name) {
		t.Fatalf("work dir = %q, want %q", got, filepath.Join(workspace, name))
	}
}

func TestCloneTargetStartsFromBaseWhenOriginBranchMissing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r := gitRunner()
	useTestWorkspaceRoot(t)
	cloneBase := t.TempDir()
	gitCache := t.TempDir()
	name := "target-branch-base"
	mainRev, _ := seedBranchResumeRepo(t, cloneBase, name)

	work, err := r.cloneTarget(context.Background(), bootstrapEnv{
		TargetOwner: "owner",
		TargetName:  name,
		ForgejoBase: "https://forgejo.example",
		CloneBase:   cloneBase,
		GitCache:    gitCache,
		MirrorName:  "mirror.git",
		Branch:      "issue-999",
	})
	if err != nil {
		t.Fatalf("cloneTarget: %v", err)
	}
	if got := mustGitRev(t, work, "HEAD"); got != mainRev {
		t.Fatalf("HEAD = %s, want base rev %s", got, mainRev)
	}
	if got := gitText(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != "issue-999" {
		t.Fatalf("current branch = %q, want %q", got, "issue-999")
	}
}

func TestCloneExtraRepoResumesExistingOriginBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r := gitRunner()
	useTestWorkspaceRoot(t)
	cloneBase := t.TempDir()
	gitCache := t.TempDir()
	name := "extra-branch-resume"
	branch := "issue-735"
	mainRev, branchRev := seedBranchResumeRepo(t, cloneBase, name)

	r.cloneExtraRepo(context.Background(), bootstrapEnv{
		ForgejoBase: "https://forgejo.example",
		CloneBase:   cloneBase,
		GitCache:    gitCache,
		Branch:      branch,
	}, targetRepo{Owner: "owner", Name: name}, false, "")

	work := filepath.Join(workspaceRoot, "owner", name)
	if got := mustGitRev(t, work, "HEAD"); got != branchRev {
		t.Fatalf("HEAD = %s, want resumed branch rev %s", got, branchRev)
	}
	if got := gitText(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
		t.Fatalf("current branch = %q, want %q", got, branch)
	}
	if got := mustGitRev(t, work, "origin/"+branch); got != branchRev {
		t.Fatalf("origin/%s = %s, want %s", branch, got, branchRev)
	}
	if got := mustGitRev(t, work, "origin/main"); got != mainRev {
		t.Fatalf("origin/main = %s, want %s", got, mainRev)
	}
}

func TestCloneExtraRepoStartsFromBaseWhenOriginBranchMissing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r := gitRunner()
	useTestWorkspaceRoot(t)
	cloneBase := t.TempDir()
	gitCache := t.TempDir()
	name := "extra-branch-base"
	mainRev, _ := seedBranchResumeRepo(t, cloneBase, name)

	r.cloneExtraRepo(context.Background(), bootstrapEnv{
		ForgejoBase: "https://forgejo.example",
		CloneBase:   cloneBase,
		GitCache:    gitCache,
		Branch:      "issue-999",
	}, targetRepo{Owner: "owner", Name: name}, false, "")

	work := filepath.Join(workspaceRoot, "owner", name)
	if got := mustGitRev(t, work, "HEAD"); got != mainRev {
		t.Fatalf("HEAD = %s, want base rev %s", got, mainRev)
	}
	if got := gitText(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != "issue-999" {
		t.Fatalf("current branch = %q, want %q", got, "issue-999")
	}
}

// TestCloneExtraReposWithSameBasename proves the working-copy layout, not just
// parsing, permits organization .github repositories in one engineer workspace.
func TestCloneExtraReposWithSameBasename(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r := gitRunner()
	workspace := useTestWorkspaceRoot(t)
	cloneBase := t.TempDir()
	gitCache := t.TempDir()
	owners := []string{"coilyco-flight-deck", "coilyco-bridge"}
	for _, owner := range owners {
		seedBranchResumeRepo(t, cloneBase, ".github")
		from := filepath.Join(cloneBase, "owner", ".github.git")
		to := filepath.Join(cloneBase, owner, ".github.git")
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
	}
	e := bootstrapEnv{ForgejoBase: "https://forgejo.example", CloneBase: cloneBase, GitCache: gitCache, Branch: "issue-1526"}
	for _, owner := range owners {
		r.cloneExtraRepo(context.Background(), e, targetRepo{Owner: owner, Name: ".github"}, false, "")
	}
	for _, owner := range owners {
		work := filepath.Join(workspace, owner, ".github")
		if !isDir(filepath.Join(work, ".git")) {
			t.Errorf("%s clone missing at %s", owner, work)
		}
	}
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

func TestReadBootstrapEnvRequiresLocalHarnessConfig(t *testing.T) {
	for _, tc := range []struct {
		name, mode, model, endpoint, want string
	}{
		{name: "goose model", mode: string(modeGoose), want: "agent.goose.model"},
		{name: "opencode model", mode: string(modeOpencode), endpoint: "http://local.example/v1", want: "agent.opencode.model"},
		{name: "opencode endpoint", mode: string(modeOpencode), model: "local-model", want: "agent.opencode.endpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WARD_TARGET_OWNER", "owner")
			t.Setenv("WARD_TARGET_NAME", "repo")
			t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.example")
			t.Setenv("WARD_MODE", tc.mode)
			t.Setenv("WARD_OPENCODE_MODEL", "")
			t.Setenv("WARD_GOOSE_MODEL", "")
			t.Setenv("WARD_OLLAMA_URL", tc.endpoint)
			if tc.mode == string(modeGoose) {
				t.Setenv("WARD_GOOSE_MODEL", tc.model)
			} else {
				t.Setenv("WARD_OPENCODE_MODEL", tc.model)
			}
			_, err := readBootstrapEnv()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want missing key %q", err, tc.want)
			}
		})
	}
}

func TestReadBootstrapEnvKeepsConfiguredLocalHarnessValues(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "owner")
	t.Setenv("WARD_TARGET_NAME", "repo")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.example")
	t.Setenv("WARD_MODE", string(modeOpencode))
	t.Setenv("WARD_OPENCODE_MODEL", "deployment-model")
	t.Setenv("WARD_OLLAMA_URL", "http://deployment.example/v1")
	e, err := readBootstrapEnv()
	if err != nil {
		t.Fatalf("configured opencode bootstrap: %v", err)
	}
	if e.OpencodeModel != "deployment-model" || e.OllamaURL != "http://deployment.example/v1" {
		t.Fatalf("configured values changed: model=%q endpoint=%q", e.OpencodeModel, e.OllamaURL)
	}
}

// TestWriteForgejoGitCredentialHelper covers the read-only helper contract.
// `get` reads from the shared file, and `store` / `erase` never create a lock.
func TestWriteForgejoGitCredentialHelper(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := t.TempDir()
	credFile := filepath.Join(tmp, "ward-git-credentials")
	helper := filepath.Join(tmp, "ward-git-credential-helper")
	if err := writeForgejoGitCredentialHelper(helper, credFile); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	if fi, err := os.Stat(helper); err != nil {
		t.Fatalf("helper missing: %v", err)
	} else if fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("helper is not executable: %v", fi.Mode())
	}
	body, err := os.ReadFile(helper)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}
	for _, want := range []string{
		"case \"${1:-}\" in",
		"git credential-store --file=\"$cred_file\" \"$@\"",
		"store|erase)",
		"exit 0",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("helper missing %q:\n%s", want, body)
		}
	}
	seed := "protocol=https\nhost=forgejo.example\nusername=coilyco-ops\npassword=plain-text\n\n"
	store := exec.Command("git", "credential-store", "--file="+credFile, "store")
	store.Stdin = strings.NewReader(seed)
	if out, err := store.CombinedOutput(); err != nil {
		t.Fatalf("seed credential file: %v\n%s", err, out)
	}
	get := exec.Command(helper, "get")
	get.Stdin = strings.NewReader("protocol=https\nhost=forgejo.example\n\n")
	out, err := get.CombinedOutput()
	if err != nil {
		t.Fatalf("helper get: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{"username=coilyco-ops", "password=plain-text"} {
		if !strings.Contains(got, want) {
			t.Errorf("helper get output missing %q:\n%s", want, got)
		}
	}
	lock := credFile + ".lock"
	for _, action := range []string{"store", "erase"} {
		cmd := exec.Command(helper, action)
		cmd.Stdin = strings.NewReader(seed)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper %s: %v\n%s", action, err, out)
		}
		if _, err := os.Stat(lock); !os.IsNotExist(err) {
			t.Fatalf("helper %s created lock file %s (err=%v)", action, lock, err)
		}
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

func TestComposeClaudeSettingsInjectsStatusLineOnlyForClaude(t *testing.T) {
	base := []byte(`{"tui":"fullscreen","permissions":{"defaultMode":"bypassPermissions"}}`)
	gotClaude := composeClaudeSettings(modeClaude, base)
	if !strings.Contains(string(gotClaude), `"statusLine"`) {
		t.Fatalf("claude settings missing statusLine:\n%s", gotClaude)
	}
	gotCodex := composeClaudeSettings(modeCodex, base)
	if strings.Contains(string(gotCodex), `"statusLine"`) {
		t.Fatalf("codex settings should not gain statusLine:\n%s", gotCodex)
	}
}

func TestPrepareScratchSpace(t *testing.T) {
	r := &Runner{}
	scratch := t.TempDir()
	t.Setenv("TMPDIR", "")
	t.Setenv("TMP", "")
	t.Setenv("TEMP", "")
	t.Setenv("GOCACHE", "")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOTMPDIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if err := r.prepareScratchSpace(scratch); err != nil {
		t.Fatalf("prepareScratchSpace: %v", err)
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got := os.Getenv(key); got != scratch {
			t.Errorf("%s = %q, want %s", key, got, scratch)
		}
	}
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOTMPDIR", "XDG_CACHE_HOME"} {
		if got := os.Getenv(key); !strings.HasPrefix(got, scratch+string(os.PathSeparator)) {
			t.Errorf("%s = %q, want a subdir under %s", key, got, scratch)
		}
	}
	if info, err := os.Stat(scratch); err != nil || !info.IsDir() {
		t.Fatalf("%s not provisioned: %v", scratch, err)
	}
}

func TestPrepareScratchSpaceLowBudget(t *testing.T) {
	r := &Runner{}
	scratch := t.TempDir()
	prev := surfaceScratchDiskFreeBytes
	surfaceScratchDiskFreeBytes = func(string) (uint64, uint64, error) {
		return surfaceScratchFloorBytes - 1, surfaceScratchFloorBytes, nil
	}
	t.Cleanup(func() { surfaceScratchDiskFreeBytes = prev })

	err := r.prepareScratchSpace(scratch)
	if err == nil {
		t.Fatal("prepareScratchSpace should refuse a low-budget scratch root")
	}
	for _, want := range []string{
		"⚠️",
		"Docker resource constraint",
		scratch,
		"focused Go verification",
		"recommended cache/temp location",
		diskBytes(surfaceScratchFloorBytes),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("low-budget error %q missing %q", err, want)
		}
	}
}

func TestSurfaceScratchDir(t *testing.T) {
	if got := surfaceScratchDir(bootstrapEnv{GitCache: "/gitcache"}); got != "/scratch" {
		t.Fatalf("surfaceScratchDir(writable) = %q, want /scratch", got)
	}
	if got := surfaceScratchDir(bootstrapEnv{GitCache: "/gitcache", ReadOnly: true}); got != filepath.Join("/gitcache", "surface-scratch") {
		t.Fatalf("surfaceScratchDir(read-only) = %q, want %q", got, filepath.Join("/gitcache", "surface-scratch"))
	}
}

func TestEnsureScratchAlias(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "gitcache", "surface-scratch")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ensureScratchAlias(scratch, scratch); err != nil {
		t.Fatalf("same-path alias should be a no-op: %v", err)
	}

	alias := filepath.Join(root, "scratch")
	if err := ensureScratchAlias(scratch, alias); err != nil {
		t.Fatalf("ensureScratchAlias: %v", err)
	}
	if target, err := os.Readlink(alias); err != nil || target != scratch {
		t.Fatalf("alias = %q (%v), want symlink to %s", target, err, scratch)
	}
	// The issue's verification probe: a write through the alias must land.
	if err := os.WriteFile(filepath.Join(alias, "probe"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write through alias: %v", err)
	}

	if err := ensureScratchAlias(scratch, alias); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}

	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "stale")
	if err := os.Symlink(other, stale); err != nil {
		t.Fatal(err)
	}
	if err := ensureScratchAlias(scratch, stale); err != nil {
		t.Fatalf("repoint stale alias: %v", err)
	}
	if target, _ := os.Readlink(stale); target != scratch {
		t.Fatalf("stale alias now %q, want %s", target, scratch)
	}

	realDir := filepath.Join(root, "realdir")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureScratchAlias(scratch, realDir); err != nil {
		t.Fatalf("existing dir alias: %v", err)
	}
	if fi, err := os.Lstat(realDir); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("existing real dir at the alias should be left alone (err=%v)", err)
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
