package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
)

// forge.go makes the git-hosting service a first-class dimension of a `ward agent`
// run (ward#489): Forgejo (canonical) or GitHub. See docs/agent-github.md.

// githubBaseURL is the GitHub origin clones + issue URLs build from; the token
// resolves user-side, never here (ward#489, #441).
const githubBaseURL = "https://github.com"

// forge identifies which hosting service a ref/clone/issue-thread targets. The zero
// value forgeForgejo keeps every forge-unaware ref, plan, and env on Forgejo.
type forge int

const (
	forgeForgejo forge = iota // ward's canonical home forge (default)
	forgeGitHub               // the public front door (ward#489)
)

// String renders the forge as the lowercase token used on the WARD_FORGE env and
// in log lines; it round-trips through parseForge.
func (f forge) String() string {
	if f == forgeGitHub {
		return "github"
	}
	return "forgejo"
}

// baseURL is the TARGET repo's clone + issue-URL origin, distinct from the always
// -Forgejo base ward downloads its own release/broker from (WARD_FORGEJO_BASE).
func (f forge) baseURL() string {
	if f == forgeGitHub {
		return githubBaseURL
	}
	return forgejoBaseURL
}

// host is the bare hostname the git credential-store line keys on.
func (f forge) host() string { return forgejoHostFromBase(f.baseURL()) }

// gitPushUser is the username half of the https credential line: the coilyco-ops bot
// for Forgejo (ward#245), or x-access-token for GitHub (PAT or App token; ward#489).
func (f forge) gitPushUser() string {
	if f == forgeGitHub {
		return "x-access-token"
	}
	return "coilyco-ops"
}

// parseForge maps the WARD_FORGE token back to a forge, defaulting to Forgejo for
// an empty or unknown value so a missing env can never flip an existing run.
func parseForge(s string) forge {
	if strings.EqualFold(strings.TrimSpace(s), "github") {
		return forgeGitHub
	}
	return forgeForgejo
}

// errForgeLockUnsupported is the sentinel lockIssue/unlockIssue return when the forge
// API has no lock leaf (Forgejo); the caller falls back to the comment (ward#494).
var errForgeLockUnsupported = errors.New("this forge's API cannot lock an issue conversation")

// issueForge is the forge-independent issue-thread surface the host dispatch path
// and reaper drive: Forgejo (forgejoClient) or GitHub via `gh` (githubClient).
type issueForge interface {
	getIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error)
	listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error)
	createIssue(ctx context.Context, owner, repo, title, body string) (int, error)
	commentIssue(ctx context.Context, owner, repo string, number int, body string) error
	closeIssue(ctx context.Context, owner, repo string, number int) error
	reopenIssue(ctx context.Context, owner, repo string, number int) error
	// lockIssue seals the conversation against in-flight steering (ward#494), returning
	// errForgeLockUnsupported where the API has no lock leaf; unlockIssue retracts it.
	lockIssue(ctx context.Context, owner, repo string, number int) error
	unlockIssue(ctx context.Context, owner, repo string, number int) error
}

// hostForgeClient returns the issue-thread client for f, signing writes as mode.
// Forgejo routes through the in-binary ops mount; GitHub shells out to `gh`.
func (r *Runner) hostForgeClient(ctx context.Context, f forge, mode containerMode) (issueForge, error) {
	if f == forgeGitHub {
		return r.hostGitHubClient(mode)
	}
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return nil, err
	}
	return cl.withMode(mode), nil
}

// githubTokenSource selects how the GitHub dispatch path provisions its token
// (ward#533): env (the publishable default), gh, or app. See docs/agent-github.md.
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

// parseGitHubTokenSource maps a WARD_GITHUB_TOKEN_SOURCE value to a source (empty is
// the env default); an unrecognized value is a hard error, never a silent fall-through.
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

// resolveGitHubToken provisions the GitHub token for clone/push + `gh`, dispatching on
// WARD_GITHUB_TOKEN_SOURCE; owner/repo scope the app arm (ward#489/533/534).
func (r *Runner) resolveGitHubToken(ctx context.Context, owner, repo string) (string, error) {
	src, err := parseGitHubTokenSource(os.Getenv("WARD_GITHUB_TOKEN_SOURCE"))
	if err != nil {
		return "", err
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
			"ward reads GitHub tokens only from the environment, never SSM (ward#489). See docs/agent-github.md")
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
