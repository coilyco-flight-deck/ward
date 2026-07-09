package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func TestCodexProbeArgvIncludesModel(t *testing.T) {
	got := codexProbeArgv(agentsapi.RunCtx{CodexModel: "gpt-5.4"})
	for _, want := range []string{"codex", "exec", "--skip-git-repo-check", "--ephemeral", "--ignore-user-config", "--sandbox", "danger-full-access", "--model", "gpt-5.4", "-"} {
		found := false
		for _, arg := range got {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("probe argv missing %q: %v", want, got)
		}
	}
}

func TestCodexProbeArgvUsesAgentHome(t *testing.T) {
	rc := agentsapi.RunCtx{
		CodexModel: "gpt-5.4",
		AgentHome:  "/home/testuser",
		AgentUID:   "1000",
		AgentGID:   "1000",
	}
	got := codexProbeArgv(rc)

	if len(got) < 7 {
		t.Fatalf("probe argv should include setpriv prefix, got: %v", got)
	}

	privArgs := []string{"setpriv", "--reuid=1000", "--regid=1000", "--init-groups"}
	for _, arg := range privArgs {
		found := false
		for _, g := range got {
			if g == arg {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("probe argv missing setpriv argument %q: %v", arg, got)
		}
	}

	envArgs := []string{"env", "HOME=/home/testuser", "CODEX_HOME=/home/testuser/.codex"}
	for _, arg := range envArgs {
		found := false
		for _, g := range got {
			if g == arg {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("probe argv missing env argument %q: %v", arg, got)
		}
	}
}

func TestCodexProbeArgvUsesAgentHomeWithoutSetprivWhenNoIdentity(t *testing.T) {
	rc := agentsapi.RunCtx{CodexModel: "gpt-5.4", AgentHome: "/home/testuser"}
	got := codexProbeArgv(rc)

	for _, forbidden := range []string{"setpriv", "--init-groups"} {
		for _, arg := range got {
			if arg == forbidden {
				t.Fatalf("probe argv should not include %q without uid/gid: %v", forbidden, got)
			}
		}
	}
	for _, want := range []string{"env", "HOME=/home/testuser", "CODEX_HOME=/home/testuser/.codex", "codex", "exec"} {
		found := false
		for _, arg := range got {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("probe argv missing %q: %v", want, got)
		}
	}
}

func TestClassifyCodexProbeFailure(t *testing.T) {
	err := classifyCodexProbeFailure("gpt-5.4", "", `warning: Model metadata for gpt-5.4 not found. Defaulting to fallback metadata; this can degrade performance and cause issues.
ERROR: {"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The 'gpt-5.4' model is not supported when using Codex with a ChatGPT account."}}`, 1)
	if err == nil {
		t.Fatal("expected model-config error")
	}
	if got := err.(interface{ GateName() string }).GateName(); got != "model-config" {
		t.Fatalf("GateName = %q, want model-config", got)
	}
	if !strings.Contains(err.Error(), "gpt-5.4") {
		t.Fatalf("error should name the model, got %v", err)
	}
	if err := classifyCodexProbeFailure("gpt-5.4", "", "codex auth missing", 1); err == nil {
		t.Fatal("non-model failure should still block the codex probe")
	} else if got := err.(interface{ GateName() string }).GateName(); got != codexProbeGate {
		t.Fatalf("generic codex probe failure gate = %q, want %q", got, codexProbeGate)
	}
	if err := classifyCodexProbeFailure("gpt-5.4", "", "Reading additional input from stdin...\nOpenAI Codex v0.142.5", -1); err == nil {
		t.Fatal("stdin-stall failure should still block the codex probe")
	} else if got := err.(interface{ GateName() string }).GateName(); got != codexProbeGate {
		t.Fatalf("stdin-stall gate = %q, want %q", got, codexProbeGate)
	} else if !strings.Contains(err.Error(), "feeds the prompt on stdin") {
		t.Fatalf("stdin-stall error should carry recovery guidance, got %v", err)
	}
}

func TestCodexPreLaunchNoOps(t *testing.T) {
	if err := (Agent{}).PreLaunchCheck(agentsapi.RunCtx{Ctx: context.Background(), Headless: false}); err != nil {
		t.Fatalf("non-headless PreLaunchCheck should no-op, got %v", err)
	}
}

func TestCodexPreLaunchUsesStdinPrompt(t *testing.T) {
	binDir := t.TempDir()
	src := filepath.Join(binDir, "codexmain.go")
	binName := "codex"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(binDir, binName)
	program := `package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "-" {
		fmt.Fprintln(os.Stderr, "missing stdin prompt sentinel")
		os.Exit(2)
	}
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	if got := strings.TrimSpace(string(in)); got != "Reply with exactly ok." {
		fmt.Fprintf(os.Stderr, "stdin = %q", got)
		os.Exit(4)
	}
	fmt.Print("ok")
}
`
	if err := os.WriteFile(src, []byte(program), 0o600); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, src)
	cmd.Dir = binDir
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper binary: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	agent := Agent{}
	rc := agentsapi.RunCtx{
		Ctx:        context.Background(),
		Headless:   true,
		AgentHome:  t.TempDir(),
		CodexModel: "gpt-5.4",
		Log:        func(string, ...any) {},
	}
	if err := agent.PreLaunchCheck(rc); err != nil {
		t.Fatalf("PreLaunchCheck with stdin prompt should pass, got %v", err)
	}
}
