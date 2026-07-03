package ollamaprobe

// doctor.go is the host-side pre-dispatch face of the launch gate's reachability
// check: ward doctor calls DoctorProbe (ward#499). See docs/doctor.md.

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	// ProbeName is the ward doctor row label and --skip key, referenced by core as a
	// symbol so no per-agent literal lands in cmd/ward (the ward#425 ratchet).
	ProbeName = "ollama"
	// OpencodeEndpointEnv / GooseHostEnv are the same env the dispatch path binds:
	// opencode's endpoint override and goose's base64 endpoint (SSM-resolved host-side).
	OpencodeEndpointEnv = "WARD_OLLAMA_URL"
	GooseHostEnv        = "WARD_GOOSE_OLLAMA_HOST_B64"
)

// Doctor row severities, mirroring ward doctor's probeResult severities so core
// compares against these symbols instead of re-embedding the strings.
const (
	SevPass = "PASS"
	SevFail = "FAIL"
	SevSkip = "SKIP"
)

// Target is one resolved local-harness Ollama endpoint the host-side probe dials.
type Target struct {
	Harness  string // "opencode" | "goose"
	Endpoint string // the URL/host:port the dispatch path would bind
	Source   string // human-readable origin, e.g. "$WARD_OLLAMA_URL"
}

// DoctorRow is one rendered probe outcome; core drives the exit code off SevFail.
type DoctorRow struct {
	Severity string
	Detail   string
}

// DoctorTargets gathers the operator-configured local-harness endpoints from the env
// the dispatch path binds. Empty means none configured. See docs/doctor.md.
func DoctorTargets(getenv func(string) string) []Target {
	var out []Target
	if v := strings.TrimSpace(getenv(OpencodeEndpointEnv)); v != "" {
		out = append(out, Target{Harness: "opencode", Endpoint: v, Source: "$" + OpencodeEndpointEnv})
	}
	if v := strings.TrimSpace(getenv(GooseHostEnv)); v != "" {
		if dec, err := base64.StdEncoding.DecodeString(v); err == nil {
			if h := strings.TrimSpace(string(dec)); h != "" {
				out = append(out, Target{Harness: "goose", Endpoint: h, Source: "$" + GooseHostEnv})
			}
		}
	}
	return out
}

// DoctorProbe resolves the configured endpoints and dials each once - what core calls.
// ProbeTargets is the injectable-reacher seam its unit tests drive.
func DoctorProbe(ctx context.Context, getenv func(string) string) []DoctorRow {
	return ProbeTargets(DoctorTargets(getenv), func(endpoint string) (string, error) {
		return ReachOnce(ctx, endpoint)
	})
}

// ProbeTargets dials each target through reach, rendering a row per endpoint or a
// single SKIP when none is configured (the baked default is left to the launch gate).
func ProbeTargets(targets []Target, reach func(endpoint string) (addr string, err error)) []DoctorRow {
	if len(targets) == 0 {
		return []DoctorRow{{Severity: SevSkip, Detail: skipDetail()}}
	}
	out := make([]DoctorRow, 0, len(targets))
	for _, t := range targets {
		addr, err := reach(t.Endpoint)
		if err != nil {
			out = append(out, DoctorRow{Severity: SevFail, Detail: failDetail(t, err)})
			continue
		}
		out = append(out, DoctorRow{
			Severity: SevPass,
			Detail:   fmt.Sprintf("%s endpoint %s (%s) reachable at %s", t.Harness, t.Endpoint, t.Source, addr),
		})
	}
	return out
}

// skipDetail is the no-endpoint-configured SKIP message, naming the opt-in env var.
func skipDetail() string {
	return "no local-harness Ollama endpoint configured this session " +
		"($" + OpencodeEndpointEnv + " / $" + GooseHostEnv + " unset); goose/opencode dispatch " +
		"would fall back to the baked default and the in-container launch gate probes it there. " +
		"Set $" + OpencodeEndpointEnv + " to have doctor probe it here first (ward#499)"
}

// failDetail is the unreachable-endpoint FAIL message, naming the endpoint and bypass.
func failDetail(t Target, err error) string {
	return fmt.Sprintf("%s endpoint %s (%s) unreachable: %s - a headless %s dispatch would "+
		"hang on the launch gate instead of running. Is Ollama up and reachable from this host? "+
		"%s=1 bypasses the gate (ward#487/#499)",
		t.Harness, t.Endpoint, t.Source, oneLineErr(err), t.Harness, skipEnv)
}

// oneLineErr collapses an error message to a single line for per-row rendering.
func oneLineErr(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
