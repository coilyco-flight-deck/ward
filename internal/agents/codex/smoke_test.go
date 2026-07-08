package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func TestCodexProbeArgvIncludesModel(t *testing.T) {
	got := codexProbeArgv(agentsapi.RunCtx{CodexModel: "gpt-5.4"})
	for _, want := range []string{"codex", "exec", "--skip-git-repo-check", "--ephemeral", "--ignore-user-config", "--sandbox", "danger-full-access", "--model", "gpt-5.4", "Reply with exactly ok."} {
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
}

func TestCodexPreLaunchNoOps(t *testing.T) {
	if err := (Agent{}).PreLaunchCheck(agentsapi.RunCtx{Ctx: context.Background(), Headless: false}); err != nil {
		t.Fatalf("non-headless PreLaunchCheck should no-op, got %v", err)
	}
}
