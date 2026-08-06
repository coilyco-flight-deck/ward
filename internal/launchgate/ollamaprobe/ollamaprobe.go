// Package ollamaprobe is the local-model harnesses' pre-launch reachability gate:
// a bounded TCP probe of the configured Ollama endpoint so an unreachable model
// backend fails loud before launch instead of hanging the dispatched container.
// It is the goose/opencode analog of claude's auth smoke test (ward#487, mirroring
// ward#222): claude probes its credential, the local harnesses probe their model
// host. See docs/agent-local-harnesses.md.
package ollamaprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
	"github.com/coilyco-flight-deck/ward/internal/launchgate/modelconfig"
)

const (
	// OpencodeEndpointEnv / GooseHostEnv are the same env the dispatch path binds.
	// They live here because the local-harness launch gate still consumes them.
	OpencodeEndpointEnv = "WARD_OLLAMA_URL"
	GooseHostEnv        = "WARD_GOOSE_OLLAMA_HOST_B64"
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
	return preLaunch(rc, harness, endpoint, "", nil)
}

// PreLaunchModel extends PreLaunch with a native Ollama model existence check.
func PreLaunchModel(rc agentsapi.RunCtx, harness, endpoint, model string) error {
	return preLaunch(rc, harness, endpoint, model, modelExists)
}

// PreLaunchOpenAIModel checks models through the OpenAI-compatible API used by
// providers whose base URL ends in /v1.
func PreLaunchOpenAIModel(rc agentsapi.RunCtx, harness, endpoint, model string) error {
	return preLaunch(rc, harness, endpoint, model, openAIModelExists)
}

type modelExistenceProbe func(context.Context, string, string) error

func preLaunch(rc agentsapi.RunCtx, harness, endpoint, model string, modelProbe modelExistenceProbe) error {
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
		return fmt.Errorf("ollama smoke test: %s's ollama endpoint %s (%s) was unreachable after %s - the dispatched container would hang or fail opaquely instead of a clean abort (ward#487, the local-harness analog of claude's auth smoke test). Is the backend reachable from the container? Point %s at a live endpoint (opencode: the agent-proxy URL in WARD_OLLAMA_URL; goose: its configured Ollama host) or pass --ts-sidecar to route localhost:11434 to the tower. %s=1 bypasses. (last dial error: %w)", harness, endpoint, addr, probeWindow, harness, skipEnv, derr)
	}
	if model = strings.TrimSpace(model); model != "" && modelProbe != nil {
		if merr := modelProbe(rc.Ctx, endpoint, model); merr != nil {
			return agentsapi.NewGateError(modelconfig.GateName, fmt.Errorf("ollama smoke test: %s configured model %q is stale for %s (ward#670): %s. update the configured model or endpoint", harness, model, endpoint, oneLineModelErr(merr)))
		}
	}
	rc.Log("ollama smoke test: %s ollama endpoint reachable, proceeding", harness)
	return nil
}

// modelExists probes the native Ollama model list.
func modelExists(ctx context.Context, endpoint, model string) error {
	tagsURL, err := modelTagsURL(endpoint)
	if err != nil {
		return err
	}
	return modelExistsAt(ctx, tagsURL, model)
}

// openAIModelExists probes the OpenAI-compatible model list used by Opencode.
func openAIModelExists(ctx context.Context, endpoint, model string) error {
	modelsURL, err := openAIModelsURL(endpoint)
	if err != nil {
		return err
	}
	return modelExistsAt(ctx, modelsURL, model)
}

type advertisedModel struct {
	Name  string `json:"name"`
	Model string `json:"model"`
	ID    string `json:"id"`
}

func modelExistsAt(ctx context.Context, modelsURL, model string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %s from %s: %s", resp.Status, modelsURL, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		Models []advertisedModel `json:"models"`
		Data   []advertisedModel `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("decode %s: %w", modelsURL, err)
	}
	for _, m := range append(parsed.Models, parsed.Data...) {
		for _, cand := range []string{m.Name, m.Model, m.ID} {
			if modelMatches(cand, model) {
				return nil
			}
		}
	}
	return fmt.Errorf("model %q not listed by %s", model, modelsURL)
}

func modelTagsURL(endpoint string) (string, error) {
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
	path := strings.TrimSuffix(u.Path, "/")
	path = strings.TrimSuffix(path, "/v1")
	if path == "" {
		u.Path = "/api/tags"
	} else {
		u.Path = path + "/api/tags"
	}
	return u.String(), nil
}

func openAIModelsURL(endpoint string) (string, error) {
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
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" {
		u.Path = "/v1/models"
	} else {
		u.Path = path + "/models"
	}
	return u.String(), nil
}

func modelMatches(candidate, want string) bool {
	candidate = strings.TrimSpace(candidate)
	want = strings.TrimSpace(want)
	return candidate == want || candidate == "ollama/"+want || strings.TrimPrefix(candidate, "ollama/") == want
}

func oneLineModelErr(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
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

// ReachOnce normalises endpoint and TCP-dials it once, returning the host:port it
// dialed: the host-side pre-dispatch mirror of the launch gate (ward#499).
func ReachOnce(ctx context.Context, endpoint string) (string, error) {
	addr, err := dialAddr(endpoint)
	if err != nil {
		return "", err
	}
	return addr, dialOnce(ctx, addr)
}

// dialOnce performs a single bounded TCP dial of addr, the shared reachability unit
// under both probe (the windowed launch-gate loop) and ReachOnce (the host-side probe).
func dialOnce(ctx context.Context, addr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
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
		lastErr = dialOnce(ctx, addr)
		if lastErr == nil {
			return nil
		}
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
