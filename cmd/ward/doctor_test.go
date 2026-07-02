package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/allowlist"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
)

// writeWardYAML writes body to a .ward/ward.yaml under a fresh temp dir and
// returns the path, so a security-check test needs no Makefile on disk.
func writeWardYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".ward", "ward.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// commandsOnlyYAML is a valid config with no security: block - the ward#450 case.
const commandsOnlyYAML = `commands:
  build:
    run: make build
    description: Build all packages.
`

// TestRunSecurityAt_MissingSecurityFails locks the ward#450 contract: no
// security: block fails, and the message names the block and the schema doc.
func TestRunSecurityAt_MissingSecurityFails(t *testing.T) {
	path := writeWardYAML(t, commandsOnlyYAML)
	var out bytes.Buffer
	err := runSecurityAt(&out, path, doctorOptions{})
	if err == nil {
		t.Fatal("want a failure when no security: block is declared, got nil")
	}
	for _, want := range []string{"security:", "docs/ward-yaml.md", "ward#450"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("failure message missing %q\ngot: %s", want, err.Error())
		}
	}
}

// TestRunSecurityAt_MissingSecurityAllowed covers the ward setup path: the same
// config is a NOTE, not a failure, when allowMissingSecurity is set.
func TestRunSecurityAt_MissingSecurityAllowed(t *testing.T) {
	path := writeWardYAML(t, commandsOnlyYAML)
	var out bytes.Buffer
	if err := runSecurityAt(&out, path, doctorOptions{allowMissingSecurity: true}); err != nil {
		t.Fatalf("allowMissingSecurity should not fail: %v", err)
	}
	if !strings.Contains(out.String(), "NOTE") || !strings.Contains(out.String(), "docs/ward-yaml.md") {
		t.Errorf("expected a remediation NOTE naming the schema doc, got:\n%s", out.String())
	}
}

func TestRenderAllowlistFailure_AnchorsAndHint(t *testing.T) {
	problems := []allowlist.Problem{
		{File: ".ward/ward.yaml", Line: 2, Msg: "commands.build has no matching Makefile target"},
		{File: ".ward/ward.yaml", Line: 3, Msg: "commands.test has no matching Makefile target"},
	}
	got := renderAllowlistFailure(problems)

	// Every problem stays anchored to file:line: message.
	for _, want := range []string{
		".ward/ward.yaml:2: commands.build has no matching Makefile target",
		".ward/ward.yaml:3: commands.test has no matching Makefile target",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered failure missing %q\ngot:\n%s", want, got)
		}
	}
	// The contract hint is appended once, after the problems, so an adopter
	// staring at a valid-looking bare target learns why it reads as unmatched.
	if !strings.Contains(got, "## <description>") || !strings.Contains(got, "docs/doctor.md") {
		t.Errorf("rendered failure missing the contract hint\ngot:\n%s", got)
	}
	if n := strings.Count(got, "hint:"); n != 1 {
		t.Errorf("want exactly one hint, got %d\n%s", n, got)
	}
	if lines := strings.Split(got, "\n"); lines[len(lines)-1] != allowlistContractHint {
		t.Errorf("hint should be the last line, got last = %q", lines[len(lines)-1])
	}
}

func TestSummarizeSecurity_Empty(t *testing.T) {
	got := summarizeSecurity(repocfg.Security{})
	want := "ward doctor security: no security: declared"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSummarizeSecurity_Populated(t *testing.T) {
	sec := repocfg.Security{
		ProtectedBinaries: []repocfg.ProtectedBinary{
			{Name: "gcloud"},
			{Name: "aws"},
		},
		Sudo: repocfg.SudoPolicy{ForbidPasswordless: true},
		Hooks: repocfg.HookPolicy{
			DenyBareBinaries: []string{"gcloud"},
			RouteHints:       map[string]string{"gcloud": "Use kap for cloud operations."},
		},
	}
	got := summarizeSecurity(sec)
	wantParts := []string{
		"ward doctor security:",
		"2 protected",
		"sudo=forbid_passwordless",
		"hooks=1 deny / 1 route-hint",
	}
	for _, w := range wantParts {
		if !strings.Contains(got, w) {
			t.Errorf("summary %q missing %q", got, w)
		}
	}
}

func TestSummarizeSecurity_OnlyProtected(t *testing.T) {
	sec := repocfg.Security{
		ProtectedBinaries: []repocfg.ProtectedBinary{{Name: "gcloud"}},
	}
	got := summarizeSecurity(sec)
	if !strings.Contains(got, "sudo=unrestricted") {
		t.Errorf("expected sudo=unrestricted, got %q", got)
	}
	if !strings.Contains(got, "hooks=none") {
		t.Errorf("expected hooks=none, got %q", got)
	}
}

func TestSecurityIsZero(t *testing.T) {
	if !securityIsZero(repocfg.Security{}) {
		t.Fatal("zero-value Security must be zero")
	}
	non := repocfg.Security{ProtectedBinaries: []repocfg.ProtectedBinary{{Name: "x"}}}
	if securityIsZero(non) {
		t.Fatal("Security with a protected binary must not be zero")
	}
}
