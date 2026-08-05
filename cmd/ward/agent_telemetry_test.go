package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
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

func TestRedactSecretsScrubsCredentialHeadersAndResponses(t *testing.T) {
	for _, tc := range []struct {
		input  string
		secret string
	}{
		{"Authorization: Bearer arbitrary-unshaped-value", "arbitrary-unshaped-value"},
		{"Proxy-Authorization=Basic c3ludGhldGljOnNlY3JldA==", "c3ludGhldGljOnNlY3JldA=="},
		{"password=not-a-token-shaped-credential", "not-a-token-shaped-credential"},
		{"access_token=plain-opaque-value", "plain-opaque-value"},
	} {
		got := redactSecrets(tc.input)
		if strings.Contains(got, tc.secret) || !strings.Contains(got, redactionPlaceholder) {
			t.Fatalf("credential field was not redacted: %q", got)
		}
	}
}

func TestConfiguredSecretRedactorCoversInjectedConfiguredAndPatternValues(t *testing.T) {
	writeTestWardGlobalConfig(t, `
agent:
  redaction:
    env-names:
      - PRIVATE_SERVICE_TOKEN
    patterns:
      - 'tenant-secret-[0-9]+'
`)
	t.Setenv("PRIVATE_SERVICE_TOKEN", "operator-local-exact-value")
	r, err := configuredSecretRedactor(map[string]string{
		"FORGEJO_TOKEN": "synthetic-forgejo-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := "Authorization: token synthetic-forgejo-value\nPRIVATE_SERVICE_TOKEN=operator-local-exact-value\ntenant-secret-42\nsha256:1234567890abcdef"
	got := r.redact(input)
	for _, secret := range []string{"synthetic-forgejo-value", "operator-local-exact-value", "tenant-secret-42"} {
		if strings.Contains(got, secret) {
			t.Fatalf("configured redactor leaked %q in %q", secret, got)
		}
	}
	if strings.Count(got, redactionPlaceholder) != 3 {
		t.Fatalf("redaction markers = %d, want 3 in %q", strings.Count(got, redactionPlaceholder), got)
	}
	if !strings.Contains(got, "sha256:1234567890abcdef") {
		t.Fatalf("ordinary hash was mangled: %q", got)
	}
}

func TestRuntimeConfigRejectsInvalidRedactionInputs(t *testing.T) {
	for _, body := range []string{
		"agent:\n  redaction:\n    env-names: ['NOT VALID']\n",
		"agent:\n  redaction:\n    patterns: ['[unterminated']\n",
	} {
		writeTestWardGlobalConfig(t, body)
		if _, err := currentSmartDefaultsWithError(); err == nil || !strings.Contains(err.Error(), "agent.redaction") {
			t.Fatalf("currentSmartDefaultsWithError() = %v, want agent.redaction error", err)
		}
	}
}

func TestRedactingLineWriterCatchesCredentialSplitAcrossWrites(t *testing.T) {
	secret := "synthetic-split-credential"
	r, err := newSecretRedactor([]string{secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := &redactingLineWriter{target: &out, redactor: r}
	_, _ = w.Write([]byte("Authorization: token synthetic-split-"))
	_, _ = w.Write([]byte("credential\nnext line"))
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), redactionPlaceholder) {
		t.Fatalf("split credential was not redacted: %q", out.String())
	}
}

func TestDispatchArtifactLogRedactsSplitInjectedCredential(t *testing.T) {
	writeTestWardGlobalConfig(t, "")
	secret := "synthetic-dispatch-credential"
	t.Setenv("FORGEJO_TOKEN", secret)
	path := filepath.Join(t.TempDir(), "console.log")
	logf, err := openDispatchArtifactLog(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = logf.Write([]byte("credential=synthetic-dispatch-"))
	_, _ = logf.Write([]byte("credential\n"))
	if err := logf.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) || !strings.Contains(string(body), redactionPlaceholder) {
		t.Fatalf("dispatch archive leaked split credential: %q", body)
	}
}

func TestDispatchArtifactLogRejectsInvalidRedactionPattern(t *testing.T) {
	writeTestWardGlobalConfig(t, "agent:\n  redaction:\n    patterns: ['[unterminated']\n")
	path := filepath.Join(t.TempDir(), "console.log")
	if _, err := openDispatchArtifactLog(path); err == nil || !strings.Contains(err.Error(), "agent.redaction") {
		t.Fatalf("openDispatchArtifactLog() = %v, want agent.redaction error", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid redaction config created an artifact; stat err = %v", err)
	}
}

func TestRedactJSONStringsCoversNestedMetadata(t *testing.T) {
	secret := "synthetic-metadata-credential"
	r, err := newSecretRedactor([]string{secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{
		"error":  "failed with " + secret,
		"nested": []any{map[string]any{"path": "/tmp/" + secret}},
	}
	var output map[string]any
	if err := redactJSONStrings(input, &output, r); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(output)
	if strings.Contains(string(body), secret) || strings.Count(string(body), redactionPlaceholder) != 2 {
		t.Fatalf("nested JSON was not fully redacted: %s", body)
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
		`{"type":"assistant","timestamp":"2026-06-26T02:00:01Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"git push origin HEAD:main # ghs_1234567890abcdefghijklmnopqrstuvwxyz"}}]}}`,
		// The Bash result errors, after 2s.
		`{"type":"user","timestamp":"2026-06-26T02:00:03Z","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"fatal: leaked ghs_1234567890abcdefghijklmnopqrstuvwxyz in result body"}]}}`,
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
	if strings.Contains(bash.Args["command"], "ghs_") {
		t.Errorf("Bash command leaked the token: %q", bash.Args["command"])
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

// TestRedactConsole scrubs secret shapes from a drained console while preserving
// its line structure, the redacted-at-rest console view (ward#526).
func TestRedactConsole(t *testing.T) {
	console := []byte("starting run\nleaked ghp_1234567890abcdefghijklmnopqrstuvwxyz here\nand ghs_1234567890abcdefghijklmnopqrstuvwxyz too\ndone\n")
	got := string(redactConsole(console))
	if strings.Contains(got, "ghp_") || strings.Contains(got, "ghs_") {
		t.Errorf("redactConsole left a secret shape in: %q", got)
	}
	if !strings.Contains(got, redactionPlaceholder) {
		t.Errorf("redactConsole did not scrub anything: %q", got)
	}
	// Benign lines and the line structure survive untouched.
	if !strings.Contains(got, "starting run") || !strings.Contains(got, "done") {
		t.Errorf("redactConsole mangled benign lines: %q", got)
	}
	if lines := strings.Count(got, "\n"); lines != strings.Count(string(console), "\n") {
		t.Errorf("redactConsole changed the line count: got %d newlines", lines)
	}
	if redactConsole(nil) != nil {
		t.Error("redactConsole(nil) must return nil, not an empty scrub")
	}
}

// TestRedactedTranscript renders a drained transcript as bodies-dropped, secret-scrubbed
// envelope jsonl - one line per tool call, reusing the extractor (ward#526).
func TestRedactedTranscript(t *testing.T) {
	transcript := strings.Join([]string{
		`{"type":"assistant","timestamp":"2026-06-26T02:00:00Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/workspace/ward/x.go","content":"secret ghp_1234567890abcdefghijklmnopqrstuvwxyz body"}}]}}`,
		`{"type":"assistant","timestamp":"2026-06-26T02:00:01Z","cwd":"/workspace/ward","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"git push origin HEAD:main # ghs_1234567890abcdefghijklmnopqrstuvwxyz"}}]}}`,
	}, "\n")

	got := redactedTranscript([]byte(transcript))
	if strings.Contains(string(got), "ghp_") || strings.Contains(string(got), "ghs_") {
		t.Errorf("redactedTranscript leaked a secret: %q", got)
	}
	// The Write body must be gone entirely (dropped, not scrubbed-and-kept).
	if strings.Contains(string(got), "\"content\"") {
		t.Errorf("redactedTranscript kept a body field: %q", got)
	}
	// One valid JSON envelope per line, both tool calls present.
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d envelope lines, want 2: %q", len(lines), got)
	}
	for i, ln := range lines {
		var env toolEnvelope
		if err := json.Unmarshal([]byte(ln), &env); err != nil {
			t.Errorf("line %d is not a valid envelope: %v (%q)", i, err, ln)
		}
	}
	// A transcript with no tool calls yields nil (a goose run, an empty tree).
	if redactedTranscript([]byte("")) != nil {
		t.Error("redactedTranscript of an empty transcript must be nil")
	}
}
