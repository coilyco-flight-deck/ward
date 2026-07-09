package opencode

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
	"github.com/coilyco-flight-deck/ward/internal/launchgate/modelconfig"
)

// TestPreLaunchCheckReachable proves opencode gates on rc.OllamaURL: a headless
// run against a live listener passes, and a non-headless run never probes.
func TestPreLaunchCheckReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	rc := agentsapi.RunCtx{
		Ctx:       context.Background(),
		Headless:  true,
		Log:       noopLog,
		OllamaURL: "http://" + ln.Addr().String() + "/v1",
	}
	if err := (Agent{}).PreLaunchCheck(rc); err != nil {
		t.Errorf("PreLaunchCheck against a live endpoint should pass, got %v", err)
	}
	rc.Headless = false
	if err := (Agent{}).PreLaunchCheck(rc); err != nil {
		t.Errorf("non-headless PreLaunchCheck should no-op, got %v", err)
	}
}

func TestPreLaunchCheckModelConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3-coder:30b"}]}`))
	}))
	defer srv.Close()

	rc := agentsapi.RunCtx{
		Ctx:           context.Background(),
		Headless:      true,
		Log:           noopLog,
		OllamaURL:     srv.URL + "/v1",
		OpencodeModel: "qwen3-coder:30b",
	}
	if err := (Agent{}).PreLaunchCheck(rc); err != nil {
		t.Fatalf("PreLaunchCheck against a live model should pass, got %v", err)
	}

	rc.OpencodeModel = "missing-model"
	err := (Agent{}).PreLaunchCheck(rc)
	if err == nil {
		t.Fatal("expected model-config failure")
	}
	if got := err.(interface{ GateName() string }).GateName(); got != modelconfig.GateName {
		t.Fatalf("GateName = %q, want %q", got, modelconfig.GateName)
	}
	if !strings.Contains(err.Error(), "missing-model") {
		t.Fatalf("error should name the missing model, got %v", err)
	}
}
