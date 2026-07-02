package main

import (
	"strings"
	"testing"
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

// TestForgeCarryClause verifies the seed's carry sentence is forge-specific: GitHub
// opens a PR (never pushes main), Forgejo merges to main + closes the issue.
func TestForgeCarryClause(t *testing.T) {
	gh := forgeCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 7, Forge: forgeGitHub})
	for _, want := range []string{"gh pr create", "Closes #7", "pull request", "GITHUB_TOKEN"} {
		if !strings.Contains(gh, want) {
			t.Errorf("github carry clause missing %q: %s", want, gh)
		}
	}
	if strings.Contains(gh, "merge to main") {
		t.Errorf("github carry clause should not tell the agent to merge to main: %s", gh)
	}
	fj := forgeCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 8})
	for _, want := range []string{"merge to main", "closes #8"} {
		if !strings.Contains(fj, want) {
			t.Errorf("forgejo carry clause missing %q: %s", want, fj)
		}
	}
}

// TestResolveGitHubToken checks the env precedence and the no-SSM error path.
func TestResolveGitHubToken(t *testing.T) {
	t.Setenv("WARD_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if _, err := resolveGitHubToken(); err == nil {
		t.Fatal("resolveGitHubToken with no env token: want error, got nil")
	}
	t.Setenv("GITHUB_TOKEN", "gh-env")
	if got, err := resolveGitHubToken(); err != nil || got != "gh-env" {
		t.Fatalf("resolveGitHubToken(GITHUB_TOKEN) = %q,%v want gh-env,nil", got, err)
	}
	// WARD_GITHUB_TOKEN takes precedence over the others.
	t.Setenv("WARD_GITHUB_TOKEN", "ward-gh")
	if got, _ := resolveGitHubToken(); got != "ward-gh" {
		t.Errorf("WARD_GITHUB_TOKEN should win, got %q", got)
	}
}
