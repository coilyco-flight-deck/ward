package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/cli/shell"
)

// github_ops.go is ward's GitHub issue-thread client (ward#489): shells `gh` to
// mirror forgejoClient's verbs behind Tracker (auth from env, no token on argv).

// Reads and state flips route through `gh api /repos/...` (REST budget), never
// GraphQL `gh issue view/close/reopen` (ward#466; see docs/compat-surface.md).

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
	if c != nil && c.r != nil && c.r.Runner != nil {
		return c.r.Runner.Capture(ctx, "gh", args...)
	}
	return (&shell.Runner{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}).Capture(ctx, "gh", args...)
}

// slug renders the --repo argument gh expects.
func ghSlug(owner, repo string) string { return owner + "/" + repo }

// getIssue reads one issue via REST `gh api /repos/{o}/{r}/issues/{n}` (ward#466)
// and maps it to Issue; ToLower keeps state matching Forgejo's "open".
func (c *githubClient) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	out, err := c.run(ctx, "api", ghIssuePath(owner, repo, number))
	if err != nil {
		return nil, fmt.Errorf("github: get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	var raw struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		State     string `json:"state"`
		HTMLURL   string `json:"html_url"`
		UpdatedAt string `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("github: parse issue %s/%s#%d: %w", owner, repo, number, err)
	}
	issue := &Issue{
		Number: raw.Number,
		Title:  raw.Title,
		Body:   raw.Body,
		State:  strings.ToLower(raw.State),
		URL:    raw.HTMLURL,
		Labels: nil,
	}
	issue.User.Login = raw.User.Login
	if t, err := time.Parse(time.RFC3339, raw.UpdatedAt); err == nil {
		issue.UpdatedAt = t
	}
	// Populate the label names so the automation-mode ceiling gate can
	// read them; GitHub labels are objects, not strings.
	for _, l := range raw.Labels {
		issue.Labels = append(issue.Labels, l.Name)
	}
	return issue, nil
}

// ghComment is one row of the REST `.../issues/{n}/comments` array (ward#466):
// REST names the fields created_at/user, not the GraphQL createdAt/author.
type ghComment struct {
	ID        int    `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// listIssueComments fetches the thread (oldest first) via REST `gh api .../comments`
// (ward#466). --paginate + per_page=100 read a long thread whole, a short one in one hit.
func (c *githubClient) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	out, err := c.run(ctx, "api", "--paginate", "-f", "per_page=100",
		ghIssuePath(owner, repo, number)+"/comments")
	if err != nil {
		return nil, fmt.Errorf("github: list comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	var comments []ghComment
	if err := json.Unmarshal(out, &comments); err != nil {
		return nil, fmt.Errorf("github: parse comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	return ghCommentsToIssueComments(comments), nil
}

// ghCommentsToIssueComments maps REST comment rows to issueComment, parsing the
// RFC3339 timestamp (a bad stamp degrades to the zero time, never an error). Pure.
func ghCommentsToIssueComments(raw []ghComment) []issueComment {
	out := make([]issueComment, 0, len(raw))
	for _, rc := range raw {
		ic := issueComment{ID: rc.ID, Body: rc.Body}
		ic.User.Login = rc.User.Login
		if t, err := time.Parse(time.RFC3339, rc.CreatedAt); err == nil {
			ic.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, rc.UpdatedAt); err == nil {
			ic.UpdatedAt = t
		}
		out = append(out, ic)
	}
	return out
}

func ghPullRequestPath(owner, repo string, number int) string {
	return "/repos/" + owner + "/" + repo + "/pulls/" + strconv.Itoa(number)
}

func (c *githubClient) GetPullRequestContext(ctx context.Context, owner, repo string, number int) (*agentPullRequestContext, error) {
	out, err := c.run(ctx, "api", ghPullRequestPath(owner, repo, number))
	if err != nil {
		return nil, fmt.Errorf("github: get pull request %s/%s#%d: %w", owner, repo, number, err)
	}
	var raw struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		State     string `json:"state"`
		HTMLURL   string `json:"html_url"`
		UpdatedAt string `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Draft     bool `json:"draft"`
		Mergeable any  `json:"mergeable"`
		Head      struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("github: parse pull request %s/%s#%d: %w", owner, repo, number, err)
	}
	var mergeability string
	switch v := raw.Mergeable.(type) {
	case bool:
		mergeability = fmt.Sprintf("mergeable=%t", v)
	case nil:
		mergeability = "mergeable=unknown"
	default:
		mergeability = fmt.Sprintf("mergeable=%v", v)
	}
	if mergeability == "" {
		mergeability = "mergeable=unknown"
	}
	if raw.Draft {
		mergeability += ", draft=true"
	}
	return &agentPullRequestContext{
		State:        strings.TrimSpace(raw.State),
		Title:        raw.Title,
		Body:         raw.Body,
		URL:          strings.TrimSpace(raw.HTMLURL),
		UpdatedAt:    parseAnyRFC3339(raw.UpdatedAt),
		Author:       strings.TrimSpace(raw.User.Login),
		HeadSHA:      strings.TrimSpace(raw.Head.SHA),
		HeadRef:      strings.TrimSpace(raw.Head.Ref),
		BaseRef:      strings.TrimSpace(raw.Base.Ref),
		Mergeability: mergeability,
	}, nil
}

func parseAnyRFC3339(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	return time.Time{}
}

func (c *githubClient) ListPullRequestComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	return c.ListIssueComments(ctx, owner, repo, number)
}

// createIssue opens an issue (signed body via --body-file, off argv) and returns its
// number, parsed from the issue URL gh prints.
func (c *githubClient) CreateIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
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
func (c *githubClient) CommentIssue(ctx context.Context, owner, repo string, number int, body string) error {
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

func (c *githubClient) DeleteIssueComment(ctx context.Context, owner, repo string, commentID int) error {
	if _, err := c.run(ctx, "api", "-X", "DELETE", ghIssueCommentPath(owner, repo, commentID)); err != nil {
		return fmt.Errorf("github: delete issue comment %s/%s#%d: %w", owner, repo, commentID, err)
	}
	return nil
}

// closeIssue flips an issue to closed via REST PATCH (ward#466: `gh issue close`
// would route through a GraphQL mutation; the REST state flip stays on the REST budget).
func (c *githubClient) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "api", "-X", "PATCH", ghIssuePath(owner, repo, number), "-f", "state=closed"); err != nil {
		return fmt.Errorf("github: close issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// reopenIssue flips a closed issue back open (the reaper's undo of a `closes #N`),
// via REST PATCH for the same rate-limit reason as closeIssue (ward#466).
func (c *githubClient) ReopenIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "api", "-X", "PATCH", ghIssuePath(owner, repo, number), "-f", "state=open"); err != nil {
		return fmt.Errorf("github: reopen issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// lockIssue seals the conversation via REST `PUT .../issues/{n}/lock` (ward#494); no
// lock_reason is sent since the API's fixed set has no "in progress" value. See docs.
func (c *githubClient) LockIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "api", "-X", "PUT", ghIssuePath(owner, repo, number)+"/lock"); err != nil {
		return fmt.Errorf("github: lock issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// unlockIssue retracts the lock via REST `DELETE .../lock` when a reservation releases.
func (c *githubClient) UnlockIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "api", "-X", "DELETE", ghIssuePath(owner, repo, number)+"/lock"); err != nil {
		return fmt.Errorf("github: unlock issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// ghIssuePath renders the REST issue path `/repos/{owner}/{repo}/issues/{n}` shared
// by every REST call in this client. Pure + testable.
func ghIssuePath(owner, repo string, number int) string {
	return "/repos/" + owner + "/" + repo + "/issues/" + strconv.Itoa(number)
}

func ghIssueCommentPath(owner, repo string, commentID int) string {
	return "/repos/" + owner + "/" + repo + "/issues/comments/" + strconv.Itoa(commentID)
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
