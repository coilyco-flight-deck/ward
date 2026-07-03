package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestForgejoExtraheaderEnvScopesTokenToForgejo asserts the injected config is a
// single Forgejo-scoped extraheader carrying a Basic-auth line for the token.
func TestForgejoExtraheaderEnvScopesTokenToForgejo(t *testing.T) {
	env := forgejoExtraheaderEnv("", "s3cr3t-token")

	if got := env["GIT_CONFIG_COUNT"]; got != "1" {
		t.Fatalf("GIT_CONFIG_COUNT = %q, want 1", got)
	}
	wantKey := "http." + forgejoBaseURL + "/.extraheader"
	if got := env["GIT_CONFIG_KEY_0"]; got != wantKey {
		t.Errorf("GIT_CONFIG_KEY_0 = %q, want %q", got, wantKey)
	}
	if !strings.HasPrefix(forgejoBaseURL, "https://") || !strings.Contains(wantKey, "forgejo.coilysiren.me") {
		t.Errorf("extraheader key %q not scoped to Forgejo", wantKey)
	}

	val := env["GIT_CONFIG_VALUE_0"]
	const prefix = "Authorization: Basic "
	if !strings.HasPrefix(val, prefix) {
		t.Fatalf("GIT_CONFIG_VALUE_0 = %q, want a %q header", val, prefix)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(val, prefix))
	if err != nil {
		t.Fatalf("basic-auth payload not base64: %v", err)
	}
	want := forgeForgejo.gitPushUser() + ":s3cr3t-token"
	if string(raw) != want {
		t.Errorf("basic-auth decodes to %q, want %q", raw, want)
	}
}

// TestForgejoExtraheaderEnvOffsetsExistingCount checks the entry appends past a
// caller's own GIT_CONFIG_COUNT rather than clobbering their KEY_0/VALUE_0.
func TestForgejoExtraheaderEnvOffsetsExistingCount(t *testing.T) {
	env := forgejoExtraheaderEnv("2", "tok")

	if got := env["GIT_CONFIG_COUNT"]; got != "3" {
		t.Errorf("GIT_CONFIG_COUNT = %q, want 3", got)
	}
	if _, ok := env["GIT_CONFIG_KEY_2"]; !ok {
		t.Errorf("expected the injected entry at index 2, got keys %v", env)
	}
	if _, ok := env["GIT_CONFIG_KEY_0"]; ok {
		t.Errorf("must not overwrite the caller's GIT_CONFIG_KEY_0")
	}
}

// TestParseGitConfigCount covers the malformed/empty/negative inputs that must
// all fall back to a zero offset.
func TestParseGitConfigCount(t *testing.T) {
	cases := map[string]int{
		"":     0,
		"  ":   0,
		"0":    0,
		"3":    3,
		" 4 ":  4,
		"-1":   0,
		"nope": 0,
	}
	for in, want := range cases {
		if got := parseGitConfigCount(in); got != want {
			t.Errorf("parseGitConfigCount(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestGitPushLeafIsNetVerb asserts push/fetch/pull are flagged net (so auth is
// injected) while a read-only verb like status is not.
func TestGitPushLeafIsNetVerb(t *testing.T) {
	net := map[string]bool{}
	for _, v := range gitPassthroughVerbs {
		net[v.name] = v.net
	}
	for _, name := range []string{"push", "fetch", "pull"} {
		if !net[name] {
			t.Errorf("git %s should be a net verb (auth injected)", name)
		}
	}
	for _, name := range []string{"status", "log", "add", "remote"} {
		if net[name] {
			t.Errorf("git %s must not inject auth (no remote)", name)
		}
	}
}
