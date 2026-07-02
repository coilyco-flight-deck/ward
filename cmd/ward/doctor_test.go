package main

import (
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/allowlist"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/repocfg"
)

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
