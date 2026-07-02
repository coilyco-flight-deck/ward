package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"aws", "key AKIAIOSFODNN7EXAMPLE here"},
		{"github-classic", "tok ghp_1234567890abcdefghijklmnopqrstuvwxyz here"},
		{"github-pat", "github_pat_" + strings.Repeat("a", 82) + " end"},
		{"anthropic", "sk-ant-api03-" + strings.Repeat("a", 95) + " end"},
		{"openai", "sk-" + strings.Repeat("b", 48) + " end"},
		{"slack", "xapp-1-ABC-123-deadbeef here"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N here"},
		{"public-ip", "host 8.8.8.8 reached"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactSecrets(c.in)
			if !strings.Contains(got, redactionPlaceholder) {
				t.Errorf("redactSecrets(%q) = %q, expected a %s", c.in, got, redactionPlaceholder)
			}
		})
	}
}

func TestRedactSecretsLeavesBenignText(t *testing.T) {
	in := "git push origin HEAD:main && echo done"
	if got := redactSecrets(in); got != in {
		t.Errorf("redactSecrets mangled benign text: %q -> %q", in, got)
	}
}

func TestRedactSecretsScrubsPrivateIPNeverButPublicYes(t *testing.T) {
	// RFC1918 + loopback are not opaque ids (the Warp list deliberately excludes them).
	for _, ip := range []string{"127.0.0.1", "10.0.0.5", "192.168.1.1", "172.16.0.1"} {
		if strings.Contains(redactSecrets("addr "+ip), redactionPlaceholder) {
			t.Errorf("private/loopback IP %s should NOT be redacted", ip)
		}
	}
}

// TestExtractEnvelopesDropsBodiesAndRedacts is the core slice-2 guarantee: tool
// RESULTS / bodies never enter an envelope, and the args that do are redacted.
func TestExtractEnvelopesDropsBodiesAndRedacts(t *testing.T) {
	transcript := strings.Join([]string{
		// A Write whose content body carries a token, plus a file path.
		`{"type":"assistant","timestamp":"2026-06-26T02:00:00Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/workspace/ward/x.go","content":"secret ghp_1234567890abcdefghijklmnopqrstuvwxyz body"}}]}}`,
		// A Bash that pushes + echoes a token in its command (an arg, so redacted).
		`{"type":"assistant","timestamp":"2026-06-26T02:00:01Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"git push origin HEAD:main # AKIAIOSFODNN7EXAMPLE"}}]}}`,
		// The Bash result errors, after 2s.
		`{"type":"user","timestamp":"2026-06-26T02:00:03Z","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"fatal: leaked AKIAIOSFODNN7EXAMPLE in result body"}]}}`,
	}, "\n")

	envs := extractEnvelopes([]byte(transcript), true)
	if len(envs) != 2 {
		t.Fatalf("got %d envelopes, want 2: %+v", len(envs), envs)
	}

	write, bash := envs[0], envs[1]

	// Body field must be absent entirely.
	if _, ok := write.Args["content"]; ok {
		t.Errorf("Write envelope leaked the content body: %v", write.Args)
	}
	// The file path is captured as a touched file.
	if len(write.Files) != 1 || write.Files[0] != "/workspace/ward/x.go" {
		t.Errorf("Write files = %v, want [/workspace/ward/x.go]", write.Files)
	}
	// No secret material anywhere in the Write envelope's kept args.
	for k, v := range write.Args {
		if strings.Contains(v, "ghp_") {
			t.Errorf("Write arg %q leaked a token: %q", k, v)
		}
	}

	// The Bash command is kept but redacted, and classified as the push step.
	if !strings.Contains(bash.Args["command"], redactionPlaceholder) {
		t.Errorf("Bash command not redacted: %q", bash.Args["command"])
	}
	if strings.Contains(bash.Args["command"], "AKIA") {
		t.Errorf("Bash command leaked the AWS id: %q", bash.Args["command"])
	}
	if bash.Lifecycle != lifecyclePush {
		t.Errorf("Bash lifecycle = %q, want %q", bash.Lifecycle, lifecyclePush)
	}
	if bash.Outcome != "failure" {
		t.Errorf("Bash outcome = %q, want failure (result was is_error)", bash.Outcome)
	}
	if bash.DurationMs != 2000 {
		t.Errorf("Bash duration = %d ms, want 2000", bash.DurationMs)
	}
	if write.Outcome != "success" {
		t.Errorf("Write outcome = %q, want success (no error result)", write.Outcome)
	}
}

// TestExtractEnvelopesFullKeepsBodies is the local full-detail counterpart: with
// redact=false the body args ride verbatim, unredacted (ward#532).
func TestExtractEnvelopesFullKeepsBodies(t *testing.T) {
	transcript := []byte(`{"type":"assistant","timestamp":"2026-06-26T02:00:00Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/workspace/ward/x.go","content":"full body ghp_1234567890abcdefghijklmnopqrstuvwxyz kept"}}]}}`)
	envs := extractEnvelopes(transcript, false)
	if len(envs) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envs))
	}
	body := envs[0].Args["content"]
	if body == "" {
		t.Fatal("full extraction dropped the content body; ward#532 requires bodies kept locally")
	}
	if !strings.Contains(body, "ghp_") {
		t.Errorf("full extraction redacted a local body it should keep verbatim: %q", body)
	}
	if len(envs[0].Files) != 1 || envs[0].Files[0] != "/workspace/ward/x.go" {
		t.Errorf("full extraction lost the touched file: %v", envs[0].Files)
	}
}

func TestOtlpLogsPayloadShape(t *testing.T) {
	envs := []toolEnvelope{{
		Tool: "Bash", Outcome: "success", Lifecycle: lifecyclePush, DurationMs: 12,
		Cwd: "/workspace/ward", Args: map[string]string{"command": "git push"},
	}}
	meta := runMeta{Container: "ward-x", Repo: "coilyco-flight-deck/ward", Driver: "claude", Issue: "363"}

	payload, err := otlpLogsPayload(envs, meta)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	s := string(payload)
	for _, want := range []string{"resourceLogs", "ward-agent", "scopeLogs", "logRecords", `"verb"`, `"outcome"`, `"duration_ms"`} {
		if !strings.Contains(s, want) {
			t.Errorf("OTLP payload missing %q:\n%s", want, s)
		}
	}
}

func TestIsLocalEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		want     bool
	}{
		{"http://localhost:4318/v1/logs", true},
		{"http://127.0.0.1:4318/v1/logs", true},
		{"http://[::1]:4318/v1/logs", true},
		{"https://localhost/v1/logs", true},
		{"http://ser8:4318/v1/logs", false},
		{"http://10.0.0.5:4318/v1/logs", false}, // private LAN is still off-box
		{"http://signoz.internal:4318/v1/logs", false},
		{"", false},
		{"not a url", false},
	}
	for _, c := range cases {
		if got := isLocalEndpoint(c.endpoint); got != c.want {
			t.Errorf("isLocalEndpoint(%q) = %v, want %v", c.endpoint, got, c.want)
		}
	}
}

// TestSignozPayloadLocalityGate is the load-bearing safety assertion (ward#532):
// full content reaches ONLY a local endpoint; a remote one gets redacted envelopes.
func TestSignozPayloadLocalityGate(t *testing.T) {
	// A Write whose body carries a token + a path, and console output with a secret.
	transcript := []byte(`{"type":"assistant","timestamp":"2026-06-26T02:00:00Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/workspace/ward/x.go","content":"body-secret ghp_1234567890abcdefghijklmnopqrstuvwxyz here"}}]}}`)
	console := []byte("agent booting\nleaked AKIAIOSFODNN7EXAMPLE in console line\nreap: landed on main\n")
	meta := runMeta{Container: "ward-x", Repo: "coilyco-flight-deck/ward", Driver: "claude", Issue: "532"}

	// Remote endpoint => redacted: no body, no raw console, no secret material.
	remote, full, n, err := signozPayload("http://ser8:4318/v1/logs", console, transcript, meta)
	if err != nil {
		t.Fatal(err)
	}
	if full {
		t.Fatal("remote endpoint classified as full; the locality gate leaked")
	}
	rs := string(remote)
	for _, banned := range []string{"body-secret", "ghp_", "AKIA", "agent booting", "reap: landed on main"} {
		if strings.Contains(rs, banned) {
			t.Errorf("remote payload leaked %q off-box:\n%s", banned, rs)
		}
	}
	if n == 0 {
		t.Error("remote payload carried no records; the redacted envelope was dropped")
	}

	// Local endpoint => full allowed: the console lines and the kept body ride.
	local, full, n, err := signozPayload("http://localhost:4318/v1/logs", console, transcript, meta)
	if err != nil {
		t.Fatal(err)
	}
	if !full {
		t.Fatal("local endpoint not classified as full; full detail was suppressed")
	}
	ls := string(local)
	for _, want := range []string{"body-secret", "agent booting", "reap: landed on main", "/workspace/ward/x.go"} {
		if !strings.Contains(ls, want) {
			t.Errorf("local full payload missing %q:\n%s", want, ls)
		}
	}
	if n < 2 {
		t.Errorf("local payload carried %d records; want console lines + the envelope", n)
	}
}

// TestShipToSignozPostsToLocal asserts the drain actually POSTs to the resolved
// endpoint and that the default endpoint is the local collector (full path).
func TestShipToSignozPostsToLocal(t *testing.T) {
	var hits int32
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// httptest binds loopback, so srv.URL is a local endpoint -> full path.
	t.Setenv(envTelemetryEndpoint, srv.URL)
	transcript := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`)
	console := []byte("hello from the agent\n")
	r := &Runner{}
	r.shipToSignoz(context.Background(), console, transcript, runMeta{Container: "ward-x"})

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("shipToSignoz POSTed %d time(s); want 1", got)
	}
	if !strings.Contains(string(gotBody), "hello from the agent") {
		t.Errorf("local POST omitted the console line:\n%s", gotBody)
	}
}

// TestShipToSignozSkipsEmpty asserts an empty drain ships nothing.
func TestShipToSignozSkipsEmpty(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv(envTelemetryEndpoint, srv.URL)
	r := &Runner{}
	r.shipToSignoz(context.Background(), nil, nil, runMeta{Container: "ward-x"})
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("shipToSignoz POSTed %d time(s) for an empty drain; want 0", got)
	}
}
