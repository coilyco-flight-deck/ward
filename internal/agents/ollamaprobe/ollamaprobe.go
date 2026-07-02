// Package ollamaprobe is the local-model harnesses' pre-launch reachability gate:
// a bounded TCP probe of the configured Ollama endpoint so an unreachable model
// backend fails loud before launch instead of hanging the dispatched container.
// It is the goose/opencode analog of claude's auth smoke test (ward#487, mirroring
// ward#222): claude probes its credential, the local harnesses probe their model
// host. See docs/agent-local-harnesses.md.
package ollamaprobe

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

const (
	// skipEnv shares claude's smoke-test bypass switch so one lever silences every
	// pre-launch gate (synced with entrypoint.sh's WARD_SMOKE_TEST_SKIP).
	skipEnv = "WARD_SMOKE_TEST_SKIP"
	// defaultOllamaPort is Ollama's listen port, assumed when an endpoint carries
	// no explicit one.
	defaultOllamaPort = "11434"
)

// probeWindow/dialTimeout/retryDelay are vars (not consts) so tests can shrink the
// window; retries absorb the --ts-sidecar forwarder's startup race (ward#359).
var (
	probeWindow = 15 * time.Second
	dialTimeout = 3 * time.Second
	retryDelay  = 1 * time.Second
)

// PreLaunch is the headless local-model reachability gate: it probes endpoint and
// errors when unreachable so the container aborts clean instead of hanging (ward#487).
func PreLaunch(rc agentsapi.RunCtx, harness, endpoint string) error {
	if !rc.Headless {
		return nil
	}
	if os.Getenv(skipEnv) == "1" {
		rc.Log("ollama smoke test skipped (%s=1)", skipEnv)
		return nil
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		rc.Log("ollama smoke test: no %s ollama endpoint configured, skipping reachability probe (ward#487)", harness)
		return nil
	}
	addr, err := dialAddr(endpoint)
	if err != nil {
		rc.Log("ollama smoke test: could not parse %s ollama endpoint %q (%v), skipping reachability probe (ward#487)", harness, endpoint, err)
		return nil
	}
	rc.Log("ollama smoke test: probing %s ollama endpoint %s before launch (ward#487)", harness, endpoint)
	if derr := probe(rc.Ctx, addr); derr != nil {
		return fmt.Errorf("ollama smoke test: %s's ollama endpoint %s (%s) was unreachable after %s - the dispatched container would hang or fail opaquely instead of a clean abort (ward#487, the local-harness analog of claude's auth smoke test). Is Ollama running and reachable from the container? Point %s at a live endpoint (opencode: WARD_OLLAMA_URL; goose: the SSM tower host /coilysiren/ollama/host) or pass --ts-sidecar to route localhost:11434 to the tower. %s=1 bypasses. (last dial error: %w)", harness, endpoint, addr, probeWindow, harness, skipEnv, derr)
	}
	rc.Log("ollama smoke test: %s ollama endpoint reachable, proceeding", harness)
	return nil
}

// dialAddr normalises an Ollama endpoint (a URL or a bare host[:port]) into the
// host:port a TCP probe dials, defaulting scheme to http and port to 11434.
func dialAddr(endpoint string) (string, error) {
	s := strings.TrimSpace(endpoint)
	if s == "" {
		return "", fmt.Errorf("empty endpoint")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in endpoint %q", endpoint)
	}
	port := u.Port()
	if port == "" {
		port = defaultOllamaPort
	}
	return net.JoinHostPort(host, port), nil
}

// probe TCP-dials addr across probeWindow, returning nil on the first connect and
// the last dial error otherwise; retries absorb the forwarder startup race (ward#359).
func probe(ctx context.Context, addr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(probeWindow)
	var lastErr error
	for {
		d := net.Dialer{Timeout: dialTimeout}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return lastErr
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(retryDelay):
		}
	}
}
