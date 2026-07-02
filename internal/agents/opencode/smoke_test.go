package opencode

import (
	"context"
	"net"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
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
