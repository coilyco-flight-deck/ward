package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func noopLog(string, ...any) {}

// TestConfigJSON keeps the literal $schema key (not interpolated) and
// interpolates the model + URL in the right places.
func TestConfigJSON(t *testing.T) {
	got, err := configJSON(agentsapi.RunCtx{
		OpencodeModel: "qwen3-coder:30b",
		OllamaURL:     "http://host.docker.internal:8082/v1",
		Correlation: agentsapi.Correlation{
			RunID:         "engineer-codex-ward-861",
			ContainerName: "engineer-codex-ward-861",
			Role:          "engineer",
			Harness:       "opencode",
			TargetRepo:    "coilyco-flight-deck/ward",
			IssueRef:      "coilyco-flight-deck/ward#861",
			Workflow:      "pull-requests-and-merge",
			ContextLevel:  "0",
			Version:       "1.2.3",
			ThreadID:      "thread-123",
		},
	})
	if err != nil {
		t.Fatalf("configJSON: %v", err)
	}
	for _, want := range []string{
		`"$schema": "https://opencode.ai/config.json"`,
		`"model": "ollama/qwen3-coder:30b"`,
		`"baseURL": "http://host.docker.internal:8082/v1"`,
		`"x-request-id": "engineer-codex-ward-861:`,
		`"x-ward-run-id": "engineer-codex-ward-861"`,
		`"x-ward-issue-ref": "coilyco-flight-deck/ward#861"`,
		`"x-ward-thread-id": "thread-123"`,
		`"qwen3-coder:30b": {}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("opencode config missing %q in:\n%s", want, got)
		}
	}
}

// TestComposeConfigWrites confirms ComposeConfig writes the config under
// ~/.config/opencode from the RunCtx model/url knobs.
func TestComposeConfigWrites(t *testing.T) {
	home := t.TempDir()
	rc := agentsapi.RunCtx{
		AgentHome:     home,
		Log:           noopLog,
		OpencodeModel: "qwen3-coder:30b",
		OllamaURL:     "http://host.docker.internal:8082/v1",
		Correlation: agentsapi.Correlation{
			RunID:      "engineer-codex-ward-861",
			Harness:    "opencode",
			TargetRepo: "coilyco-flight-deck/ward",
		},
	}
	if err := (Agent{}).ComposeConfig(rc); err != nil {
		t.Fatalf("ComposeConfig: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("expected opencode.json written: %v", err)
	}
	if !strings.Contains(string(got), `"model": "ollama/qwen3-coder:30b"`) {
		t.Errorf("opencode.json missing model:\n%s", got)
	}
	if !strings.Contains(string(got), `"x-request-id": "engineer-codex-ward-861:`) {
		t.Errorf("opencode.json missing request id:\n%s", got)
	}
}

func TestConfigJSONRejectsMissingDeploymentConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		rc   agentsapi.RunCtx
		want string
	}{
		{name: "model", rc: agentsapi.RunCtx{OllamaURL: "http://local.example/v1"}, want: "agent.opencode.model"},
		{name: "endpoint", rc: agentsapi.RunCtx{OpencodeModel: "configured-model"}, want: "agent.opencode.endpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := configJSON(tc.rc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("configJSON error = %v, want missing key %q", err, tc.want)
			}
		})
	}
}
