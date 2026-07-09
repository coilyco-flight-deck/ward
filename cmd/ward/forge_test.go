package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestParseGitHubIssueRef covers the GitHub ref/URL forms ward#489 accepts and the
// non-GitHub inputs it must leave for the Forgejo parser (ok=false).
func TestParseGitHubIssueRef(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantOK    bool
	}{
		{"https://github.com/owner/repo/issues/12", "owner", "repo", 12, true},
		{"http://github.com/owner/repo/issues/3", "owner", "repo", 3, true},
		{"github.com/owner/repo/issues/7", "owner", "repo", 7, true},
		{"github.com/owner/repo#42", "owner", "repo", 42, true},
		{"https://github.com/coilysiren/agentic-os/issues/461?foo=bar", "coilysiren", "agentic-os", 461, true},
		{"https://github.com/owner/repo.git/issues/9", "owner", "repo", 9, true},
		{"https://www.github.com/owner/repo/issues/5", "owner", "repo", 5, true},
		// Not GitHub / not an issue ref: fall through to the Forgejo parser.
		{"coilyco-flight-deck/ward#98", "", "", 0, false},
		{"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1", "", "", 0, false},
		{"github.com/owner/repo", "", "", 0, false}, // no issue number
		{"https://github.com/owner/repo/pulls/3", "", "", 0, false},
		{"#98", "", "", 0, false},
		{"github.com/owner/repo/issues/0", "", "", 0, false}, // non-positive
	}
	for _, c := range cases {
		got, ok := parseGitHubIssueRef(c.in)
		if ok != c.wantOK {
			t.Errorf("parseGitHubIssueRef(%q): ok=%v want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Owner != c.wantOwner || got.Repo != c.wantRepo || got.Number != c.wantNum {
			t.Errorf("parseGitHubIssueRef(%q) = %s/%s#%d, want %s/%s#%d", c.in, got.Owner, got.Repo, got.Number, c.wantOwner, c.wantRepo, c.wantNum)
		}
		if got.Forge != forgeGitHub {
			t.Errorf("parseGitHubIssueRef(%q): forge=%v want github", c.in, got.Forge)
		}
	}
}

// TestParseAgentIssueRefForge confirms the top-level parser tags the forge: a
// github.com ref parses to forgeGitHub, everything else stays forgeForgejo.
func TestParseAgentIssueRefForge(t *testing.T) {
	gh, err := parseAgentIssueRef("https://github.com/owner/repo/issues/1")
	if err != nil {
		t.Fatalf("parseAgentIssueRef(github URL): %v", err)
	}
	if gh.Forge != forgeGitHub {
		t.Errorf("github URL parsed to forge %v, want github", gh.Forge)
	}
	fj, err := parseAgentIssueRef("coilyco-flight-deck/ward#98")
	if err != nil {
		t.Fatalf("parseAgentIssueRef(forgejo short): %v", err)
	}
	if fj.Forge != forgeForgejo {
		t.Errorf("forgejo short ref parsed to forge %v, want forgejo", fj.Forge)
	}
}

// TestForgeURLAndBase checks the forge-selected issue URL + clone base.
func TestForgeURLAndBase(t *testing.T) {
	gh := agentIssueRef{Owner: "owner", Repo: "repo", Number: 5, Forge: forgeGitHub}
	if got, want := gh.url(), "https://github.com/owner/repo/issues/5"; got != want {
		t.Errorf("github url = %q, want %q", got, want)
	}
	fj := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 5}
	if got, want := fj.url(), forgejoBaseURL+"/coilyco-flight-deck/ward/issues/5"; got != want {
		t.Errorf("forgejo url = %q, want %q", got, want)
	}
	if forgeGitHub.baseURL() != githubBaseURL || forgeGitHub.host() != "github.com" {
		t.Errorf("github base/host = %q/%q", forgeGitHub.baseURL(), forgeGitHub.host())
	}
	if forgeGitHub.gitPushUser() != "x-access-token" || forgeForgejo.gitPushUser() != "coilyco-ops" {
		t.Errorf("git push users = %q/%q", forgeGitHub.gitPushUser(), forgeForgejo.gitPushUser())
	}
}

// TestParseForgeRoundTrip checks WARD_FORGE parsing defaults to Forgejo.
func TestParseForge(t *testing.T) {
	for _, s := range []string{"github", "GitHub", "  github  "} {
		if parseForge(s) != forgeGitHub {
			t.Errorf("parseForge(%q) != github", s)
		}
	}
	for _, s := range []string{"", "forgejo", "bogus"} {
		if parseForge(s) != forgeForgejo {
			t.Errorf("parseForge(%q) != forgejo", s)
		}
	}
	if forgeGitHub.String() != "github" || forgeForgejo.String() != "forgejo" {
		t.Errorf("String() = %q/%q", forgeGitHub.String(), forgeForgejo.String())
	}
}

// TestDirectToMainCarryClause verifies the fast path is generic across forges and
// tells the agent to land the issue on main.
func TestDirectToMainCarryClause(t *testing.T) {
	for _, ref := range []agentIssueRef{
		{Owner: "o", Repo: "r", Number: 7},
		{Owner: "o", Repo: "r", Number: 7, Forge: forgeGitHub},
	} {
		got := directToMainCarryClause(ref)
		for _, want := range []string{"merge to main", "closes #7"} {
			if !strings.Contains(got, want) {
				t.Errorf("direct-main carry clause missing %q: %s", want, got)
			}
		}
		if strings.Contains(got, "gh pr create") || strings.Contains(got, "pull request") {
			t.Errorf("direct-main carry clause should not mention a PR boundary: %s", got)
		}
	}
}

// TestResolveGitHubTokenFromEnv checks the env precedence and the no-SSM error path
// of the env source (the default, unchanged from before the ward#533 selector).
func TestResolveGitHubTokenFromEnv(t *testing.T) {
	t.Setenv("WARD_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if _, err := resolveGitHubTokenFromEnv(); err == nil {
		t.Fatal("resolveGitHubTokenFromEnv with no env token: want error, got nil")
	}
	t.Setenv("GITHUB_TOKEN", "gh-env")
	if got, err := resolveGitHubTokenFromEnv(); err != nil || got != "gh-env" {
		t.Fatalf("resolveGitHubTokenFromEnv(GITHUB_TOKEN) = %q,%v want gh-env,nil", got, err)
	}
	// WARD_GITHUB_TOKEN takes precedence over the others.
	t.Setenv("WARD_GITHUB_TOKEN", "ward-gh")
	if got, _ := resolveGitHubTokenFromEnv(); got != "ward-gh" {
		t.Errorf("WARD_GITHUB_TOKEN should win, got %q", got)
	}
}

// TestParseGitHubTokenSource maps the WARD_GITHUB_TOKEN_SOURCE token to a source,
// defaulting empty to env and rejecting an unknown value with an actionable error.
func TestParseGitHubTokenSource(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want githubTokenSource
	}{
		{"", githubTokenEnv},
		{"env", githubTokenEnv},
		{"ENV", githubTokenEnv},
		{" gh ", githubTokenGH},
		{"app", githubTokenApp},
	} {
		got, err := parseGitHubTokenSource(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("parseGitHubTokenSource(%q) = %v,%v want %v,nil", tc.in, got, err, tc.want)
		}
		if got.String() != tc.want.String() {
			t.Errorf("String() round-trip for %q: %q != %q", tc.in, got.String(), tc.want.String())
		}
	}
	if _, err := parseGitHubTokenSource("vault"); err == nil {
		t.Error("parseGitHubTokenSource(vault): want error for an unknown source, got nil")
	}
}

// TestResolveGitHubTokenSourceSelects drives every selector arm through a stubbed
// Runner (no real `gh`): env, gh (+ trim, empty, off-PATH), app, and an unknown source.
func TestResolveGitHubTokenSourceSelects(t *testing.T) {
	t.Setenv("WARD_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	t.Run("env source reads the static vars", func(t *testing.T) {
		t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "env")
		t.Setenv("GITHUB_TOKEN", "env-tok")
		r := &Runner{Runner: &shell.Runner{}}
		if got, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward"); err != nil || got != "env-tok" {
			t.Fatalf("env source = %q,%v want env-tok,nil", got, err)
		}
	})

	t.Run("gh source invokes gh auth token and trims", func(t *testing.T) {
		t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "gh")
		stub := ghAuthTokenStub(t, "  gh-minted\n")
		r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: func(string) (string, error) { return stub, nil }}}
		if got, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward"); err != nil || got != "gh-minted" {
			t.Fatalf("gh source = %q,%v want gh-minted,nil (trimmed)", got, err)
		}
	})

	t.Run("gh source with an empty token errors", func(t *testing.T) {
		t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "gh")
		stub := ghAuthTokenStub(t, "\n")
		r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: func(string) (string, error) { return stub, nil }}}
		if _, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward"); err == nil {
			t.Fatal("gh source with an empty `gh auth token`: want error, got nil")
		}
	})

	t.Run("gh source with gh off PATH errors", func(t *testing.T) {
		t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "gh")
		r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: func(string) (string, error) {
			return "", errors.New("gh: not found")
		}}}
		if _, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward"); err == nil {
			t.Fatal("gh source with gh unresolvable: want error, got nil")
		}
	})

	t.Run("app source without operator config names the missing env", func(t *testing.T) {
		t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "app")
		t.Setenv(envGitHubAppID, "")
		t.Setenv(envGitHubAppKeySSM, "")
		r := &Runner{Runner: &shell.Runner{}}
		_, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward")
		if err == nil || !strings.Contains(err.Error(), envGitHubAppID) {
			t.Fatalf("app source with no config = %v, want an error naming %s", err, envGitHubAppID)
		}
	})

	t.Run("unknown source errors before any resolution", func(t *testing.T) {
		t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "vault")
		r := &Runner{Runner: &shell.Runner{}}
		if _, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward"); err == nil {
			t.Fatal("unknown source: want error, got nil")
		}
	})
}

// ghAuthTokenStub writes a stand-in `gh` that echoes out verbatim (whitespace intact),
// standing in for `gh auth token`.
func ghAuthTokenStub(t *testing.T, out string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s' '"+out+"'\n"), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	return stub
}
