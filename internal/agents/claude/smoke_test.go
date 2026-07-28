package claude

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

func TestDiskBytes(t *testing.T) {
	cases := map[uint64]string{
		0:                       "0B",
		512:                     "512B",
		1024:                    "1.0KiB",
		1536:                    "1.5KiB",
		1024 * 1024:             "1.0MiB",
		1024 * 1024 * 1024:      "1.0GiB",
		50 * 1024 * 1024 * 1024: "50.0GiB",
	}
	for in, want := range cases {
		if got := diskBytes(in); got != want {
			t.Errorf("diskBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksLikeAuthError(t *testing.T) {
	auth := []string{
		"Error: Not logged in. Please run /login",
		"HTTP 401 Unauthorized",
		"invalid api key provided",
		`{"type":"error","error":{"type":"authentication_error"}}`,
		"UNAUTHORIZED",
	}
	for _, s := range auth {
		if !looksLikeAuthError(s) {
			t.Errorf("looksLikeAuthError(%q) = false, want true", s)
		}
	}
	notAuth := []string{
		"",
		"ENOSPC: no space left on device",
		"could not write ~/.claude/config",
		"connection timed out",
		"ok",
	}
	for _, s := range notAuth {
		if looksLikeAuthError(s) {
			t.Errorf("looksLikeAuthError(%q) = true, want false", s)
		}
	}
}

func TestClaudeProbeArgvIncludesResolvedModel(t *testing.T) {
	rc := agentsapi.RunCtx{AgentUID: "1000", AgentGID: "1000", AgentHome: "/home/agent"}
	got := claudeProbeArgv(rc)
	for _, want := range []string{"claude", "-p", "--output-format", "json", "Reply with the single word: ok"} {
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
	if got := claudeProbeArgv(agentsapi.RunCtx{AgentUID: "1000", AgentGID: "1000", AgentHome: "/home/agent", ClaudeModel: "opus"}); !strings.Contains(strings.Join(got, " "), "--model opus") {
		t.Fatalf("probe argv missing resolved model: %v", got)
	}
}

func TestClassifyClaudeModelConfigFailure(t *testing.T) {
	if err := classifyClaudeModelConfigFailure("opus", "", "Model metadata for opus not found", 1); err == nil {
		t.Fatal("expected model-config error")
	} else if got := err.(interface{ GateName() string }).GateName(); got != "model-config" {
		t.Fatalf("GateName = %q, want model-config", got)
	}
	if err := classifyClaudeModelConfigFailure("opus", "", "Not logged in. Please run /login", 1); err != nil {
		t.Fatalf("auth failure should not classify as model-config: %v", err)
	}
	if err := classifyClaudeModelConfigFailure("", "", "Model opus is not available", 1); err != nil {
		t.Fatalf("empty model should not classify: %v", err)
	}
}

func TestClassifyClaudeProbeFailure(t *testing.T) {
	cases := []struct {
		name     string
		stdout   string
		stderr   string
		want     string
		wantGate string
	}{
		{
			name:   "auth failure from stderr",
			stderr: "Error: Not logged in. Please run /login",
			want:   "rejected the credentials",
		},
		{
			name:     "quota failure from structured stdout",
			stdout:   `{"type":"error","error":{"type":"rate_limit_error","message":"Claude account token limit reached until 5pm"}}`,
			want:     "Claude account/token/quota limit",
			wantGate: claudeQuotaGate,
		},
		{
			name:     "quota failure from stderr string",
			stderr:   "Your Claude usage limit has been reached. Please try again later.",
			want:     "usage limit",
			wantGate: claudeQuotaGate,
		},
		{
			name:   "opaque exit one",
			want:   "unknown Claude prelaunch failure",
			stderr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyClaudeProbeFailure(tc.stdout, tc.stderr, 1, "/ 10.0GiB free of 20.0GiB")
			if err == nil {
				t.Fatal("expected failure")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
			gotGate, hasGate := err.(interface{ GateName() string })
			if tc.wantGate == "" {
				if hasGate {
					t.Fatalf("unexpected gate %q for %v", gotGate.GateName(), err)
				}
				return
			}
			if !hasGate || gotGate.GateName() != tc.wantGate {
				t.Fatalf("gate = %q,%v; want %q,true", func() string {
					if !hasGate {
						return ""
					}
					return gotGate.GateName()
				}(), hasGate, tc.wantGate)
			}
		})
	}
}

func TestClaudeProbeDiagnosticExtractsStructuredPayload(t *testing.T) {
	got := claudeProbeDiagnostic("", `noise
{"type":"error","status":429,"error":{"type":"rate_limit_error","message":"Claude token limit reached"}}`)
	for _, want := range []string{"rate_limit_error", "Claude token limit reached", "429"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic = %q, want it to contain %q", got, want)
		}
	}
}

func TestClaudeProbeDiagnosticRedactsSecrets(t *testing.T) {
	raw := `{"error":{"message":"failed with accessToken=\"secret-token\" and sk-ant-api03-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnop"}}`
	got := claudeProbeDiagnostic("", raw)
	for _, leaked := range []string{"secret-token", "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("diagnostic leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("diagnostic did not show redaction marker: %s", got)
	}
}

func TestDiskReportAndLowDiskPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("disk pressure reporting is implemented only on Unix hosts")
	}
	// "/" is always stat-able on the test host; a bogus path is skipped.
	rep := diskReport([]string{"/", "/ward-nonexistent-xyz"})
	if !strings.HasPrefix(rep, "/ ") || !strings.Contains(rep, "free of") {
		t.Errorf("diskReport = %q, want a '/ ... free of ...' entry", rep)
	}
	if strings.Contains(rep, "nonexistent") {
		t.Errorf("diskReport leaked an unstattable path: %q", rep)
	}

	// All-unstattable -> the sentinel, never an empty string.
	if got := diskReport([]string{"/ward-nonexistent-xyz"}); got != "disk usage unavailable" {
		t.Errorf("diskReport(bogus) = %q, want sentinel", got)
	}

	// A 0-byte floor flags nothing; a huge floor flags "/".
	if low := lowDiskPaths([]string{"/"}, 0); len(low) != 0 {
		t.Errorf("lowDiskPaths floor=0 = %v, want empty", low)
	}
	if low := lowDiskPaths([]string{"/"}, ^uint64(0)); len(low) != 1 || low[0] != "/" {
		t.Errorf("lowDiskPaths floor=max = %v, want [/]", low)
	}
}

func TestCaptureProbeStdoutStderr(t *testing.T) {
	out, errOut, rc := captureProbe(context.Background(),
		[]string{"sh", "-c", "printf hi; printf oops >&2; exit 3"})
	if strings.TrimSpace(out) != "hi" {
		t.Errorf("stdout = %q, want %q", out, "hi")
	}
	if !strings.Contains(errOut, "oops") {
		t.Errorf("stderr = %q, want it to contain %q", errOut, "oops")
	}
	if rc != 3 {
		t.Errorf("rc = %d, want 3", rc)
	}
}

func TestCapBufferCapsAtMax(t *testing.T) {
	c := &capBuffer{max: 4}
	n, err := c.Write([]byte("abcdefgh"))
	if err != nil || n != 8 {
		t.Fatalf("Write = (%d, %v), want (8, nil)", n, err)
	}
	if c.String() != "abcd" {
		t.Errorf("buffer = %q, want %q", c.String(), "abcd")
	}
	// A second write past the cap is dropped but still reports full length.
	if n, _ := c.Write([]byte("ij")); n != 2 {
		t.Errorf("second Write n = %d, want 2", n)
	}
	if c.String() != "abcd" {
		t.Errorf("buffer after overflow = %q, want %q", c.String(), "abcd")
	}
}
