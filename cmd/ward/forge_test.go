package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
		if got.Tracker != trackerGitHub {
			t.Errorf("parseGitHubIssueRef(%q): tracker=%v want github", c.in, got.Tracker)
		}
	}
}

// TestParseGitHubPullRequestRef covers the GitHub pull-request URL forms ward
// should treat as PR continuation work, not as fresh issue work.
func TestParseGitHubPullRequestRef(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantOK    bool
	}{
		{"https://github.com/owner/repo/pull/12", "owner", "repo", 12, true},
		{"https://github.com/owner/repo/pulls/12", "owner", "repo", 12, true},
		{"github.com/owner/repo/pull/7", "owner", "repo", 7, true},
		{"https://github.com/owner/repo/issues/12", "", "", 0, false},
		{"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/1", "", "", 0, false},
	}
	for _, c := range cases {
		got, ok := parseGitHubPullRequestRef(c.in)
		if ok != c.wantOK {
			t.Errorf("parseGitHubPullRequestRef(%q): ok=%v want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Owner != c.wantOwner || got.Repo != c.wantRepo || got.Number != c.wantNum {
			t.Errorf("parseGitHubPullRequestRef(%q) = %s/%s#%d, want %s/%s#%d", c.in, got.Owner, got.Repo, got.Number, c.wantOwner, c.wantRepo, c.wantNum)
		}
		if got.Forge != forgeGitHub || !got.MergeRequest {
			t.Errorf("parseGitHubPullRequestRef(%q) = %+v, want GitHub PR ref", c.in, got)
		}
	}
}

// TestParseForgejoPullRequestRef covers the Forgejo PR URL shapes ward accepts.
func TestParseForgejoPullRequestRef(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantOK    bool
	}{
		{forgejoBaseURL + "/coilyco-flight-deck/ward/pulls/12", "coilyco-flight-deck", "ward", 12, true},
		{forgejoBaseURL + "/coilyco-flight-deck/ward/pull/12", "coilyco-flight-deck", "ward", 12, true},
		{"https://github.com/owner/repo/pull/12", "", "", 0, false},
	}
	for _, c := range cases {
		got, ok := parseForgejoPullRequestRef(c.in)
		if ok != c.wantOK {
			t.Errorf("parseForgejoPullRequestRef(%q): ok=%v want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Owner != c.wantOwner || got.Repo != c.wantRepo || got.Number != c.wantNum {
			t.Errorf("parseForgejoPullRequestRef(%q) = %s/%s#%d, want %s/%s#%d", c.in, got.Owner, got.Repo, got.Number, c.wantOwner, c.wantRepo, c.wantNum)
		}
		if got.Forge != forgeForgejo || !got.MergeRequest {
			t.Errorf("parseForgejoPullRequestRef(%q) = %+v, want Forgejo PR ref", c.in, got)
		}
	}
}

// TestParseGitLabIssueRef covers the GitLab issue and merge-request URL forms
// ward#635 accepts and the non-GitLab inputs it must leave for the Forgejo parser.
func TestParseGitLabIssueRef(t *testing.T) {
	t.Setenv("WARD_GITLAB_BASE", "https://gitlab.example.com")
	cases := []struct {
		in        string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantMR    bool
		wantOK    bool
		wantURL   string
	}{
		{"https://gitlab.example.com/group/proj/-/issues/12", "group", "proj", 12, false, true, "https://gitlab.example.com/group/proj/-/issues/12"},
		{"http://gitlab.example.com/group/proj/-/issues/3", "group", "proj", 3, false, true, "https://gitlab.example.com/group/proj/-/issues/3"},
		{"gitlab.example.com/group/proj/-/merge_requests/7", "group", "proj", 7, true, true, "https://gitlab.example.com/group/proj/-/merge_requests/7"},
		{"https://gitlab.example.com/group/proj.git/-/merge_requests/42?foo=bar", "group", "proj", 42, true, true, "https://gitlab.example.com/group/proj/-/merge_requests/42"},
		// Not GitLab / not a supported ref: fall through to the Forgejo parser.
		{"https://github.com/owner/repo/issues/1", "", "", 0, false, false, ""},
		{"https://gitlab.example.com/group/proj", "", "", 0, false, false, ""},
		{"gitlab.example.com/group/proj/-/pipelines/1", "", "", 0, false, false, ""},
		{"https://gitlab.example.com/group/proj/-/issues/0", "", "", 0, false, false, ""},
	}
	for _, c := range cases {
		got, ok := parseGitLabIssueRef(c.in)
		if ok != c.wantOK {
			t.Errorf("parseGitLabIssueRef(%q): ok=%v want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Owner != c.wantOwner || got.Repo != c.wantRepo || got.Number != c.wantNum {
			t.Errorf("parseGitLabIssueRef(%q) = %s/%s#%d, want %s/%s#%d", c.in, got.Owner, got.Repo, got.Number, c.wantOwner, c.wantRepo, c.wantNum)
		}
		if got.Forge != forgeGitLab {
			t.Errorf("parseGitLabIssueRef(%q): forge=%v want gitlab", c.in, got.Forge)
		}
		if got.Tracker != trackerGitLab {
			t.Errorf("parseGitLabIssueRef(%q): tracker=%v want gitlab", c.in, got.Tracker)
		}
		if got.MergeRequest != c.wantMR {
			t.Errorf("parseGitLabIssueRef(%q): MergeRequest=%t want %t", c.in, got.MergeRequest, c.wantMR)
		}
		if got.url() != c.wantURL {
			t.Errorf("parseGitLabIssueRef(%q) url = %q, want %q", c.in, got.url(), c.wantURL)
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
	if gh.Tracker != trackerGitHub {
		t.Errorf("github URL parsed to tracker %v, want github", gh.Tracker)
	}
	t.Setenv("WARD_GITLAB_BASE", "https://gitlab.example.com")
	gl, err := parseAgentIssueRef("https://gitlab.example.com/group/proj/-/issues/1")
	if err != nil {
		t.Fatalf("parseAgentIssueRef(gitlab URL): %v", err)
	}
	if gl.Forge != forgeGitLab {
		t.Errorf("gitlab URL parsed to forge %v, want gitlab", gl.Forge)
	}
	if gl.Tracker != trackerGitLab {
		t.Errorf("gitlab URL parsed to tracker %v, want gitlab", gl.Tracker)
	}
	pr, err := parseAgentIssueRef("coilyco-flight-deck/ward!98")
	if err != nil {
		t.Fatalf("parseAgentIssueRef(forgejo pr): %v", err)
	}
	if !pr.MergeRequest || pr.Forge != forgeForgejo || pr.Tracker != trackerForgejo {
		t.Errorf("forgejo PR ref parsed to %+v, want Forgejo PR", pr)
	}
	if got, want := pr.String(), "coilyco-flight-deck/ward!98"; got != want {
		t.Errorf("forgejo PR ref string = %q, want %q", got, want)
	}
	if pr.URL != "" {
		t.Errorf("bare PR ref should not preserve a URL, got %q", pr.URL)
	}
	fj, err := parseAgentIssueRef("coilyco-flight-deck/ward#98")
	if err != nil {
		t.Fatalf("parseAgentIssueRef(forgejo short): %v", err)
	}
	if fj.Forge != forgeForgejo {
		t.Errorf("forgejo short ref parsed to forge %v, want forgejo", fj.Forge)
	}
	if fj.Tracker != trackerForgejo {
		t.Errorf("forgejo short ref parsed to tracker %v, want forgejo", fj.Tracker)
	}
}

// TestForgeURLAndBase checks the forge-selected issue URL + clone base.
func TestForgeURLAndBase(t *testing.T) {
	gh := agentIssueRef{Owner: "owner", Repo: "repo", Number: 5, Forge: forgeGitHub, Tracker: trackerGitHub}
	if got, want := gh.url(), "https://github.com/owner/repo/issues/5"; got != want {
		t.Errorf("github url = %q, want %q", got, want)
	}
	gh.MergeRequest = true
	if got, want := gh.url(), "https://github.com/owner/repo/pull/5"; got != want {
		t.Errorf("github pr url = %q, want %q", got, want)
	}
	if got, want := gh.trackerOrDefault(), trackerGitHub; got != want {
		t.Errorf("github tracker = %v, want %v", got, want)
	}
	fj := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 5}
	if got, want := fj.url(), forgejoBaseURL+"/coilyco-flight-deck/ward/issues/5"; got != want {
		t.Errorf("forgejo url = %q, want %q", got, want)
	}
	fj.MergeRequest = true
	if got, want := fj.url(), forgejoBaseURL+"/coilyco-flight-deck/ward/pulls/5"; got != want {
		t.Errorf("forgejo pr url = %q, want %q", got, want)
	}
	if got, want := fj.trackerOrDefault(), trackerForgejo; got != want {
		t.Errorf("forgejo tracker = %v, want %v", got, want)
	}
	gl := agentIssueRef{Owner: "group", Repo: "proj", Number: 9, Forge: forgeGitLab, Tracker: trackerGitLab}
	if got, want := gl.url(), "https://gitlab.com/group/proj/-/issues/9"; got != want {
		t.Errorf("gitlab issue url = %q, want %q", got, want)
	}
	glMR := agentIssueRef{Owner: "group", Repo: "proj", Number: 9, Forge: forgeGitLab, Tracker: trackerGitLab, MergeRequest: true}
	if got, want := glMR.url(), "https://gitlab.com/group/proj/-/merge_requests/9"; got != want {
		t.Errorf("gitlab mr url = %q, want %q", got, want)
	}
	if got, want := gl.trackerOrDefault(), trackerGitLab; got != want {
		t.Errorf("gitlab tracker = %v, want %v", got, want)
	}
	sc := agentIssueRef{Owner: "acme", Repo: "ward", Number: 7, Tracker: trackerShortcut, URL: "https://app.shortcut.com/acme/story/7"}
	if got, want := sc.trackerOrDefault(), trackerShortcut; got != want {
		t.Errorf("shortcut tracker = %v, want %v", got, want)
	}
	if forgeGitHub.baseURL() != githubBaseURL || forgeGitHub.host() != "github.com" {
		t.Errorf("github base/host = %q/%q", forgeGitHub.baseURL(), forgeGitHub.host())
	}
	if forgeGitHub.gitPushUser() != "x-access-token" || forgeForgejo.gitPushUser() != "coilyco-ops" {
		t.Errorf("git push users = %q/%q", forgeGitHub.gitPushUser(), forgeForgejo.gitPushUser())
	}
	t.Setenv("WARD_GITLAB_BASE", "https://gitlab.example.com/base")
	if got, want := forgeGitLab.baseURL(), "https://gitlab.example.com/base"; got != want {
		t.Errorf("gitlab base = %q, want %q", got, want)
	}
	if got, want := forgeGitLab.host(), "gitlab.example.com"; got != want {
		t.Errorf("gitlab host = %q, want %q", got, want)
	}
	if got, want := forgeGitLab.gitPushUser(), "oauth2"; got != want {
		t.Errorf("gitlab push user = %q, want %q", got, want)
	}
}

// TestParseForgeRoundTrip checks WARD_FORGE parsing defaults to Forgejo.
func TestParseForge(t *testing.T) {
	for _, s := range []string{"github", "GitHub", "  github  "} {
		if parseForge(s) != forgeGitHub {
			t.Errorf("parseForge(%q) != github", s)
		}
	}
	for _, s := range []string{"gitlab", "GitLab", "  gitlab  "} {
		if parseForge(s) != forgeGitLab {
			t.Errorf("parseForge(%q) != gitlab", s)
		}
	}
	for _, s := range []string{"", "forgejo", "bogus"} {
		if parseForge(s) != forgeForgejo {
			t.Errorf("parseForge(%q) != forgejo", s)
		}
	}
	if forgeGitHub.String() != "github" || forgeGitLab.String() != "gitlab" || forgeForgejo.String() != "forgejo" {
		t.Errorf("String() = %q/%q/%q", forgeGitHub.String(), forgeGitLab.String(), forgeForgejo.String())
	}
	if trackerShortcut.String() != "shortcut" {
		t.Errorf("shortcut tracker String() = %q, want shortcut", trackerShortcut.String())
	}
}

// TestForgeAndTrackerPairIndependence proves the git host and issue tracker can be
// set separately on the same issue ref.
func TestForgeAndTrackerPairIndependence(t *testing.T) {
	ref := agentIssueRef{Owner: "owner", Repo: "repo", Number: 9, Forge: forgeGitHub, Tracker: trackerForgejo}
	if got, want := ref.url(), forgejoBaseURL+"/owner/repo/issues/9"; got != want {
		t.Fatalf("paired ref url = %q, want %q", got, want)
	}
	if got, want := ref.trackerOrDefault(), trackerForgejo; got != want {
		t.Fatalf("paired ref tracker = %v, want %v", got, want)
	}
	if got, want := trackerFromForge(ref.Forge), trackerGitHub; got != want {
		t.Fatalf("paired ref host tracker default = %v, want %v", got, want)
	}
}

// TestResolveGitLabToken checks the env precedence and glab fallback.
func TestResolveGitLabToken(t *testing.T) {
	t.Setenv("WARD_GITLAB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_ACCESS_TOKEN", "")
	t.Setenv("OAUTH_TOKEN", "")

	if got := resolveGitLabTokenFromEnv(); got != "" {
		t.Fatalf("resolveGitLabTokenFromEnv empty = %q, want empty", got)
	}
	t.Setenv("GITLAB_TOKEN", "gl-env")
	if got := resolveGitLabTokenFromEnv(); got != "gl-env" {
		t.Fatalf("resolveGitLabTokenFromEnv(GITLAB_TOKEN) = %q, want gl-env", got)
	}
	t.Setenv("WARD_GITLAB_TOKEN", "ward-gl")
	if got := resolveGitLabTokenFromEnv(); got != "ward-gl" {
		t.Fatalf("resolveGitLabTokenFromEnv(WARD_GITLAB_TOKEN) = %q, want ward-gl", got)
	}

	t.Run("glab fallback", func(t *testing.T) {
		t.Setenv("WARD_GITLAB_TOKEN_SOURCE", "glab")
		stub := gitlabGlabTokenStub(t, "  glab-minted\n")
		t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))
		r := &Runner{Runner: &shell.Runner{Stderr: io.Discard, Resolve: func(string) (string, error) { return stub, nil }}}
		got, err := r.resolveGitLabToken(t.Context(), "group", "proj")
		if err != nil || got != "glab-minted" {
			t.Fatalf("glab source = %q,%v want glab-minted,nil", got, err)
		}
	})
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
				t.Errorf("merge-remote-main carry clause missing %q: %s", want, got)
			}
		}
		if strings.Contains(got, "gh pr create") || strings.Contains(got, "pull request") {
			t.Errorf("merge-remote-main carry clause should not mention a PR boundary: %s", got)
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

	t.Run("unset source defaults to app when provisioned", func(t *testing.T) {
		_, keyPEM := genTestKeyPEM(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/repos/coilyco/ward/installation":
				_, _ = w.Write([]byte(`{"id": 42}`))
			case r.Method == http.MethodPost && r.URL.Path == "/app/installations/42/access_tokens":
				_, _ = w.Write([]byte(`{"token":"ghs_default_app"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		orig := githubAPIBase
		githubAPIBase = srv.URL
		defer func() { githubAPIBase = orig }()

		r := awsPEMStubRunner(t, keyPEM)
		t.Setenv("WARD_GITHUB_TOKEN_SOURCE", "")
		t.Setenv(envGitHubAppID, "999")
		t.Setenv(envGitHubAppKeySSM, "/ward/github-app/key")
		if got, err := r.resolveGitHubToken(t.Context(), "coilyco", "ward"); err != nil || got != "ghs_default_app" {
			t.Fatalf("default source = %q,%v want ghs_default_app,nil", got, err)
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

func gitlabGlabTokenStub(t *testing.T, out string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "glab")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s' '"+out+"'\n"), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	return stub
}
