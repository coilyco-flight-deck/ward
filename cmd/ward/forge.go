package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// forge.go makes the git-hosting service a first-class dimension of a `ward agent`
// run (ward#489): Forgejo (canonical) or GitHub. See docs/agent-github.md.

// githubBaseURL is the GitHub origin clones + issue URLs build from; the token
// resolves user-side, never here (ward#489, #441).
const githubBaseURL = "https://github.com"

// forge identifies which git host a ref/clone/PR targets. The zero value forgeForgejo
// keeps every host-unaware ref, plan, and env on Forgejo.
type forge int

const (
	forgeForgejo forge = iota // ward's canonical home forge (default)
	forgeGitHub               // the public front door (ward#489)
	forgeGitLab               // the widest foreign git host after GitHub
)

// String renders the forge as the lowercase token used on the WARD_FORGE env and
// in log lines; it round-trips through parseForge.
func (f forge) String() string {
	switch f {
	case forgeForgejo:
		return "forgejo"
	case forgeGitHub:
		return "github"
	case forgeGitLab:
		return "gitlab"
	}
	return "forgejo"
}

// baseURL is the TARGET repo's clone + issue-URL origin, distinct from the always
// -Forgejo base ward downloads its own release/broker from (WARD_FORGEJO_BASE).
func (f forge) baseURL() string {
	switch f {
	case forgeForgejo:
		return forgejoBaseURL
	case forgeGitHub:
		return githubBaseURL
	case forgeGitLab:
		return gitlabBaseURL()
	}
	return forgejoBaseURL
}

// host is the bare hostname the git credential-store line keys on.
func (f forge) host() string { return forgejoHostFromBase(f.baseURL()) }

// gitPushUser is the username half of the https credential line: the coilyco-ops bot
// for Forgejo (ward#245), or x-access-token for GitHub (PAT or App token; ward#489).
func (f forge) gitPushUser() string {
	switch f {
	case forgeForgejo:
		return "coilyco-ops"
	case forgeGitHub:
		return "x-access-token"
	case forgeGitLab:
		return "oauth2"
	}
	return "coilyco-ops"
}

// tracker identifies which issue-thread system a ref points at. It defaults to
// Forgejo so host-unaware refs keep the current zero-config behavior.
type tracker int

const (
	trackerForgejo tracker = iota
	trackerGitHub
	trackerGitLab
	trackerShortcut
)

// String renders the tracker as the lowercase token used in logs and env-like
// strings.
func (t tracker) String() string {
	switch t {
	case trackerForgejo:
		return "forgejo"
	case trackerGitHub:
		return "github"
	case trackerGitLab:
		return "gitlab"
	case trackerShortcut:
		return "shortcut"
	}
	return "forgejo"
}

// trackerFromForge keeps the current zero-config pairing: Forgejo host -> Forgejo
// tracker, GitHub host -> GitHub tracker, GitLab host -> GitLab tracker.
func trackerFromForge(f forge) tracker {
	switch f {
	case forgeForgejo:
		return trackerForgejo
	case forgeGitHub:
		return trackerGitHub
	case forgeGitLab:
		return trackerGitLab
	}
	return trackerForgejo
}

const (
	shortcutAppBaseURL   = "https://app.shortcut.com"
	shortcutAPIBaseURL   = "https://api.app.shortcut.com/api/v3"
	shortcutWorkspaceEnv = "SHORTCUT_WORKSPACE"
	shortcutTokenEnv     = "SHORTCUT_API_TOKEN"
)

var shortcutStoryURLRE = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?app\.shortcut\.com/([^/]+)/story/(\d+)(?:/[^/?#]*)?(?:[/?#].*)?$`)
var shortcutStoryRefRE = regexp.MustCompile(`(?i)^(?:sc-|shortcut-)(\d+)$`)

func parseShortcutIssueRef(s string) (agentIssueRef, bool) {
	trimmed := strings.TrimSpace(s)
	m := shortcutStoryURLRE.FindStringSubmatch(trimmed)
	if m != nil {
		n, err := parsePositiveInt(m[2])
		if err != nil || n <= 0 {
			return agentIssueRef{}, false
		}
		return agentIssueRef{Number: n, Tracker: trackerShortcut, URL: trimmed, ShortcutWorkspace: m[1]}, true
	}
	m = shortcutStoryRefRE.FindStringSubmatch(trimmed)
	if m == nil {
		return agentIssueRef{}, false
	}
	n, err := parsePositiveInt(m[1])
	if err != nil || n <= 0 {
		return agentIssueRef{}, false
	}
	ref := agentIssueRef{Number: n, Tracker: trackerShortcut}
	if workspace := strings.TrimSpace(os.Getenv(shortcutWorkspaceEnv)); workspace != "" {
		ref.ShortcutWorkspace = workspace
	}
	return ref, true
}

// parseForge maps the WARD_FORGE token back to a forge, defaulting to Forgejo for
// an empty or unknown value so a missing env can never flip an existing run.
func parseForge(s string) forge {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "github":
		return forgeGitHub
	case "gitlab":
		return forgeGitLab
	default:
		return forgeForgejo
	}
}

// githubRefRE matches a github.com issue URL (`.../owner/repo/issues/N`) or the
// compact `github.com/owner/repo#N`, tolerating scheme/www/.git/query/fragment.
var githubRefRE = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?github\.com/([\w.-]+)/([\w.-]+?)(?:\.git)?(?:/issues/(\d+)|#(\d+))(?:[/?#].*)?$`)

// gitlabRefRE matches a GitLab issue URL or merge-request URL under the current
// configured base (gitlab.com by default), tolerating scheme/www/.git/query/fragment.
func gitlabRefRE(baseURL string) *regexp.Regexp {
	baseURL = strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://"), "/")
	return regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?` + regexp.QuoteMeta(baseURL) +
		`/([\w.-]+)/([\w.-]+?)(?:\.git)?(?:/-/(issues|merge_requests)/(\d+))(?:[/?#].*)?$`)
}

// parseGitHubIssueRef tags a github.com ref forgeGitHub; ok is false for anything
// else, so the caller falls through to the Forgejo parser. See docs/agent-github.md.
func parseGitHubIssueRef(s string) (agentIssueRef, bool) {
	m := githubRefRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return agentIssueRef{}, false
	}
	num := m[3]
	if num == "" {
		num = m[4]
	}
	n, err := parsePositiveInt(num)
	if err != nil || n <= 0 {
		return agentIssueRef{}, false
	}
	return agentIssueRef{Owner: m[1], Repo: strings.TrimSuffix(m[2], ".git"), Number: n, Forge: forgeGitHub, Tracker: trackerGitHub}, true
}

// parseGitLabIssueRef tags a GitLab issue or merge-request URL as forgeGitLab;
// ok is false for anything else, so the caller falls through to the repo parser.
func parseGitLabIssueRef(s string) (agentIssueRef, bool) {
	m := gitlabRefRE(gitlabBaseURL()).FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return agentIssueRef{}, false
	}
	n, err := parsePositiveInt(m[4])
	if err != nil || n <= 0 {
		return agentIssueRef{}, false
	}
	return agentIssueRef{Owner: m[1], Repo: strings.TrimSuffix(m[2], ".git"), Number: n, Forge: forgeGitLab, Tracker: trackerGitLab, MergeRequest: strings.EqualFold(m[3], "merge_requests")}, true
}

// parsePositiveInt parses a base-10 issue number, rejecting a leading sign or
// non-digit so a malformed capture cannot masquerade as a valid ref.
func parsePositiveInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// errForgeLockUnsupported is the sentinel lockIssue/unlockIssue return when the forge
// API has no lock leaf (Forgejo); the caller falls back to the comment (ward#494).
var errForgeLockUnsupported = errors.New("this forge's API cannot lock an issue conversation")

// Tracker is the forge-independent issue-thread surface for host dispatch and reaping.
// Forgejo, GitHub, GitLab, or Shortcut.
type Tracker interface {
	GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error)
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error)
	CreateIssue(ctx context.Context, owner, repo, title, body string) (int, error)
	CommentIssue(ctx context.Context, owner, repo string, number int, body string) error
	DeleteIssueComment(ctx context.Context, owner, repo string, commentID int) error
	CloseIssue(ctx context.Context, owner, repo string, number int) error
	ReopenIssue(ctx context.Context, owner, repo string, number int) error
	// lockIssue seals the conversation against in-flight steering (ward#494), returning
	// errForgeLockUnsupported where the API has no lock leaf; unlockIssue retracts it.
	LockIssue(ctx context.Context, owner, repo string, number int) error
	UnlockIssue(ctx context.Context, owner, repo string, number int) error
}

// hostTrackerClient returns the issue-thread client for t, signing writes as mode.
func (r *Runner) hostTrackerClient(ctx context.Context, t tracker, mode containerMode) (Tracker, error) {
	switch t {
	case trackerGitHub:
		return r.hostGitHubClient(mode)
	case trackerGitLab:
		return r.hostGitLabClient(ctx, mode), nil
	case trackerShortcut:
		return r.hostShortcutClient(mode)
	case trackerForgejo:
		cl := r.hostForgejoClient(ctx)
		return cl.withMode(mode), nil
	default:
		cl := r.hostForgejoClient(ctx)
		return cl.withMode(mode), nil
	}
}

// gitlabBaseURL is the configurable GitLab origin used for clones, issue URLs,
// and merge requests. Self-hosted instances override the default via env.
func gitlabBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("WARD_GITLAB_BASE")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://gitlab.com"
}

// githubTokenSource selects how the GitHub dispatch path provisions its token
// (ward#533): env, gh, or app. See docs/agent-github.md.
type githubTokenSource int

const (
	githubTokenEnv githubTokenSource = iota // WARD_GITHUB_TOKEN / GH_TOKEN / GITHUB_TOKEN (default)
	githubTokenGH                           // shell `gh auth token` on the host at dispatch
	githubTokenApp                          // mint from a GitHub App key (not yet built, ward#534)
)

// String renders the source as its lowercase WARD_GITHUB_TOKEN_SOURCE token.
func (s githubTokenSource) String() string {
	switch s {
	case githubTokenGH:
		return "gh"
	case githubTokenApp:
		return "app"
	case githubTokenEnv:
		return "env"
	default:
		return "env"
	}
}

// parseGitHubTokenSource maps a WARD_GITHUB_TOKEN_SOURCE value to a source;
// an unrecognized value is a hard error, never a silent fall-through.
func parseGitHubTokenSource(s string) (githubTokenSource, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "env":
		return githubTokenEnv, nil
	case "gh":
		return githubTokenGH, nil
	case "app":
		return githubTokenApp, nil
	default:
		return githubTokenEnv, fmt.Errorf(
			"ward: unknown WARD_GITHUB_TOKEN_SOURCE %q - want env (default), gh, or app. See docs/agent-github.md", strings.TrimSpace(s))
	}
}

// defaultGitHubTokenSource picks the fleet default when WARD_GITHUB_TOKEN_SOURCE is
// unset: prefer the bot-backed App path once its provisioning env is present, else env.
func defaultGitHubTokenSource() githubTokenSource {
	if strings.TrimSpace(os.Getenv(envGitHubAppID)) != "" && strings.TrimSpace(os.Getenv(envGitHubAppPrivateKey)) != "" {
		return githubTokenApp
	}
	return githubTokenEnv
}

// resolveGitHubToken provisions the GitHub token for clone/push + `gh`, dispatching on
// WARD_GITHUB_TOKEN_SOURCE; owner/repo scope the app arm (ward#489/533/534).
func (r *Runner) resolveGitHubToken(ctx context.Context, owner, repo string) (string, error) {
	raw := os.Getenv("WARD_GITHUB_TOKEN_SOURCE")
	src, err := parseGitHubTokenSource(raw)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(raw) == "" {
		src = defaultGitHubTokenSource()
	}
	switch src {
	case githubTokenGH:
		return r.resolveGitHubTokenFromGH(ctx)
	case githubTokenApp:
		return r.resolveGitHubTokenFromApp(ctx, owner, repo)
	case githubTokenEnv:
		return resolveGitHubTokenFromEnv()
	default:
		return resolveGitHubTokenFromEnv()
	}
}

// gitlabTokenSource selects how the GitLab dispatch path provisions its token:
// env or `glab` (the host-side fallback when no env token is set).
type gitlabTokenSource int

const (
	gitlabTokenEnv gitlabTokenSource = iota
	gitlabTokenGlab
)

func (s gitlabTokenSource) String() string {
	switch s {
	case gitlabTokenEnv:
		return "env"
	case gitlabTokenGlab:
		return "glab"
	}
	return "env"
}

func parseGitLabTokenSource(s string) (gitlabTokenSource, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "env":
		return gitlabTokenEnv, nil
	case "glab":
		return gitlabTokenGlab, nil
	default:
		return gitlabTokenEnv, fmt.Errorf(
			"ward: unknown WARD_GITLAB_TOKEN_SOURCE %q - want env (default) or glab. See docs/agent-gitlab.md", strings.TrimSpace(s))
	}
}

func (r *Runner) resolveGitLabToken(ctx context.Context, owner, repo string) (string, error) {
	raw := os.Getenv("WARD_GITLAB_TOKEN_SOURCE")
	src, err := parseGitLabTokenSource(raw)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(raw) == "" {
		if tok := strings.TrimSpace(resolveGitLabTokenFromEnv()); tok != "" {
			return tok, nil
		}
		if hostHasBinary("glab") {
			src = gitlabTokenGlab
		}
	}
	switch src {
	case gitlabTokenEnv:
		tok := resolveGitLabTokenFromEnv()
		if tok == "" {
			return "", fmt.Errorf("ward: set one of WARD_GITLAB_TOKEN, GITLAB_TOKEN, GITLAB_ACCESS_TOKEN, or OAUTH_TOKEN, or install `glab` for a host-side token fallback. See docs/agent-gitlab.md")
		}
		return tok, nil
	case gitlabTokenGlab:
		return r.resolveGitLabTokenFromGlab(ctx, owner, repo)
	}
	tok := resolveGitLabTokenFromEnv()
	if tok == "" {
		return "", fmt.Errorf("ward: set one of WARD_GITLAB_TOKEN, GITLAB_TOKEN, GITLAB_ACCESS_TOKEN, or OAUTH_TOKEN, or install `glab` for a host-side token fallback. See docs/agent-gitlab.md")
	}
	return tok, nil
}

func resolveGitLabTokenFromEnv() string {
	for _, key := range []string{"WARD_GITLAB_TOKEN", "GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"} {
		if tok := strings.TrimSpace(os.Getenv(key)); tok != "" {
			return tok
		}
	}
	return ""
}

func (r *Runner) resolveGitLabTokenFromGlab(ctx context.Context, owner, repo string) (string, error) {
	if !hostHasBinary("glab") {
		return "", fmt.Errorf("ward: `glab` is not on PATH; set WARD_GITLAB_TOKEN or install glab for a GitLab-hosted run")
	}
	out, err := r.Runner.Capture(ctx, "glab", "auth", "token")
	if err != nil {
		return "", fmt.Errorf("ward: resolve GitLab token with `glab auth token`: %w", err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("ward: `glab auth token` returned an empty token for %s/%s", owner, repo)
	}
	return tok, nil
}

// resolveGitHubTokenFromEnv reads the first non-empty static env var - the zero-config
// publishable default an external adopter needs nothing else for.
func resolveGitHubTokenFromEnv() (string, error) {
	for _, k := range []string{"WARD_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if tok := strings.TrimSpace(os.Getenv(k)); tok != "" {
			return tok, nil
		}
	}
	return "", fmt.Errorf(
		"ward: no GitHub token found - set GITHUB_TOKEN (or GH_TOKEN / WARD_GITHUB_TOKEN) to a token with repo scope, " +
			"or set WARD_GITHUB_TOKEN_SOURCE=gh to mint one from your `gh` login; " +
			"ward reads GitHub tokens only from explicit user-provided sources (ward#489). See docs/agent-github.md")
}

// resolveGitHubTokenFromGH shells `gh auth token` host-side for a fresh token from the
// operator's `gh` login (ward#533), trimmed; a missing/logged-out `gh` is an error.
func (r *Runner) resolveGitHubTokenFromGH(ctx context.Context) (string, error) {
	out, err := r.Runner.Capture(ctx, "gh", "auth", "token")
	if err != nil {
		return "", fmt.Errorf(
			"ward: WARD_GITHUB_TOKEN_SOURCE=gh but `gh auth token` failed - install the `gh` CLI and run `gh auth login` (or switch to WARD_GITHUB_TOKEN_SOURCE=env): %w", err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf(
			"ward: WARD_GITHUB_TOKEN_SOURCE=gh but `gh auth token` returned an empty token - run `gh auth login` (or switch to WARD_GITHUB_TOKEN_SOURCE=env). See docs/agent-github.md")
	}
	return tok, nil
}
