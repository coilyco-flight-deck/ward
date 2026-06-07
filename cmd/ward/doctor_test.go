package main

import (
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/repocfg"
)

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
