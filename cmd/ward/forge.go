package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
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

// githubRefRE matches a github.com issue URL (`.../owner/repo/issues/N`) or the
// compact `github.com/owner/repo#N`, tolerating scheme/www/.git/query/fragment.
var githubRefRE = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?github\.com/([\w.-]+)/([\w.-]+?)(?:\.git)?(?:/issues/(\d+)|#(\d+))(?:[/?#].*)?$`)

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
	return agentIssueRef{Owner: m[1], Repo: strings.TrimSuffix(m[2], ".git"), Number: n, Forge: forgeGitHub}, true
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

// issueForge is the forge-independent issue-thread surface the host dispatch path
// and reaper drive: Forgejo (forgejoClient) or GitHub via `gh` (githubClient).
type issueForge interface {
	getIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error)
	listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error)
	createIssue(ctx context.Context, owner, repo, title, body string) (int, error)
	commentIssue(ctx context.Context, owner, repo string, number int, body string) error
	closeIssue(ctx context.Context, owner, repo string, number int) error
	reopenIssue(ctx context.Context, owner, repo string, number int) error
	findOpenIssueByTitlePrefix(ctx context.Context, owner, repo, prefix string) (int, bool, error)
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

// resolveGitHubToken finds the user-supplied GitHub token from the environment for
// clone/push + `gh`. There is NO SSM fallback like Forgejo's (ward#489, #441/#453).
func resolveGitHubToken() (string, error) {
	for _, k := range []string{"WARD_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if tok := strings.TrimSpace(os.Getenv(k)); tok != "" {
			return tok, nil
		}
	}
	return "", fmt.Errorf(
		"ward: no GitHub token found - set GITHUB_TOKEN (or GH_TOKEN / WARD_GITHUB_TOKEN) to a token with repo scope; " +
			"ward reads GitHub tokens only from the environment, never SSM (ward#489). See docs/agent-github.md")
}
