package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
)

// github_ops.go is ward's GitHub issue-thread client (ward#489): shells `gh` to
// mirror forgejoClient's verbs behind issueForge (auth from env, no token on argv).

// githubListLimit caps `gh issue list` reads, matching forgejoListLimit's shallow
// pagination posture (the salvage-issue lookup never needs a deep scan).
const githubListLimit = "50"

// githubClient drives GitHub through `gh`. r runs it audited; mode signs the
// bodies it writes so GitHub comments carry the same attribution as Forgejo's.
type githubClient struct {
	r    *Runner
	mode containerMode
}

// hostGitHubClient builds the `gh`-backed client, erroring if `gh` is off PATH; the
// token resolves inside `gh` from the environment, never here.
func (r *Runner) hostGitHubClient(mode containerMode) (*githubClient, error) {
	if !hostHasBinary("gh") {
		return nil, fmt.Errorf("github: the `gh` CLI is not on PATH; a GitHub-hosted run needs it to read/write issues and open the PR (ward#489)")
	}
	return &githubClient{r: r, mode: mode}, nil
}

// run shells `gh` and returns captured stdout.
func (c *githubClient) run(ctx context.Context, args ...string) ([]byte, error) {
	return c.r.Runner.Capture(ctx, "gh", args...)
}

// slug renders the --repo argument gh expects.
func ghSlug(owner, repo string) string { return owner + "/" + repo }

// getIssue reads one issue via `gh issue view --json` and maps it to dispatch.Issue,
// lowercasing GitHub's UPPERCASE state so the "open" check matches Forgejo's.
func (c *githubClient) getIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	out, err := c.run(ctx, "issue", "view", strconv.Itoa(number),
		"--repo", ghSlug(owner, repo), "--json", "number,title,body,state,url")
	if err != nil {
		return nil, fmt.Errorf("github: get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("github: parse issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return &dispatch.Issue{
		Number: raw.Number,
		Title:  raw.Title,
		Body:   raw.Body,
		State:  strings.ToLower(raw.State),
		URL:    raw.URL,
	}, nil
}

// ghComment is one row of `gh issue view --json comments`.
type ghComment struct {
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
}

// listIssueComments fetches the comment thread (oldest first) via `gh issue view
// --json comments`, mapping it to the shared issueComment shape.
func (c *githubClient) listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	out, err := c.run(ctx, "issue", "view", strconv.Itoa(number),
		"--repo", ghSlug(owner, repo), "--json", "comments")
	if err != nil {
		return nil, fmt.Errorf("github: list comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	var wrap struct {
		Comments []ghComment `json:"comments"`
	}
	if err := json.Unmarshal(out, &wrap); err != nil {
		return nil, fmt.Errorf("github: parse comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	return ghCommentsToIssueComments(wrap.Comments), nil
}

// ghCommentsToIssueComments maps `gh` comment rows to issueComment, parsing the
// RFC3339 timestamp (a bad stamp degrades to the zero time, never an error). Pure.
func ghCommentsToIssueComments(raw []ghComment) []issueComment {
	out := make([]issueComment, 0, len(raw))
	for _, rc := range raw {
		ic := issueComment{Body: rc.Body}
		ic.User.Login = rc.Author.Login
		if t, err := time.Parse(time.RFC3339, rc.CreatedAt); err == nil {
			ic.CreatedAt = t
		}
		out = append(out, ic)
	}
	return out
}

// createIssue opens an issue (signed body via --body-file, off argv) and returns its
// number, parsed from the issue URL gh prints.
func (c *githubClient) createIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
	path, cleanup, err := writeGitHubBody(c.mode.signBody(body))
	if err != nil {
		return 0, err
	}
	defer cleanup()
	out, err := c.run(ctx, "issue", "create", "--repo", ghSlug(owner, repo),
		"--title", title, "--body-file", path)
	if err != nil {
		return 0, fmt.Errorf("github: create issue in %s/%s: %w", owner, repo, err)
	}
	n, perr := issueNumberFromURL(strings.TrimSpace(string(out)))
	if perr != nil {
		return 0, fmt.Errorf("github: parse created issue URL %q: %w", strings.TrimSpace(string(out)), perr)
	}
	return n, nil
}

// commentIssue appends a comment; the signed body rides a --body-file.
func (c *githubClient) commentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	path, cleanup, err := writeGitHubBody(c.mode.signBody(body))
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := c.run(ctx, "issue", "comment", strconv.Itoa(number),
		"--repo", ghSlug(owner, repo), "--body-file", path); err != nil {
		return fmt.Errorf("github: comment issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// closeIssue flips an issue to closed.
func (c *githubClient) closeIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "issue", "close", strconv.Itoa(number), "--repo", ghSlug(owner, repo)); err != nil {
		return fmt.Errorf("github: close issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// reopenIssue flips a closed issue back open (the reaper's undo of a `closes #N`).
func (c *githubClient) reopenIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "issue", "reopen", strconv.Itoa(number), "--repo", ghSlug(owner, repo)); err != nil {
		return fmt.Errorf("github: reopen issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// findOpenIssueByTitlePrefix returns the first open issue whose title starts with
// prefix, so the reaper appends to a salvage issue instead of filing a duplicate.
func (c *githubClient) findOpenIssueByTitlePrefix(ctx context.Context, owner, repo, prefix string) (int, bool, error) {
	out, err := c.run(ctx, "issue", "list", "--repo", ghSlug(owner, repo),
		"--state", "open", "--limit", githubListLimit, "--json", "number,title")
	if err != nil {
		return 0, false, fmt.Errorf("github: list issues in %s/%s: %w", owner, repo, err)
	}
	var issues []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(out, &issues); err != nil {
		return 0, false, fmt.Errorf("github: parse issue list for %s/%s: %w", owner, repo, err)
	}
	for _, i := range issues {
		if strings.HasPrefix(i.Title, prefix) {
			return i.Number, true, nil
		}
	}
	return 0, false, nil
}

// issueNumberFromURL pulls the trailing issue/PR number off a github.com URL like
// `https://github.com/owner/repo/issues/123`. Pure + testable.
func issueNumberFromURL(u string) (int, error) {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u == "" {
		return 0, fmt.Errorf("empty URL")
	}
	last := u[strings.LastIndexByte(u, '/')+1:]
	return parsePositiveInt(last)
}

// writeGitHubBody writes a signed markdown body to a temp file for gh's --body-file,
// returning the path and a cleanup. Keeps the body off argv, matching Forgejo's path.
func writeGitHubBody(body string) (path string, cleanup func(), err error) {
	noop := func() {}
	f, err := os.CreateTemp("", "ward-github-body-*.md")
	if err != nil {
		return "", noop, fmt.Errorf("github: create body file: %w", err)
	}
	remove := func() { _ = os.Remove(f.Name()) }
	if _, werr := f.WriteString(body); werr != nil {
		_ = f.Close()
		remove()
		return "", noop, fmt.Errorf("github: write body file: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		remove()
		return "", noop, fmt.Errorf("github: close body file: %w", cerr)
	}
	return f.Name(), remove, nil
}
