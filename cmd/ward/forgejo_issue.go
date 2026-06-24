package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
)

// forgejo_issue.go is ward's minimal, read-only Forgejo client - just enough
// for `ward agent` to resolve a Forgejo issue ref. See docs/agent.md.

// forgejoBaseURL is the Forgejo origin. A meaningful host, safe to hardcode
// (the opaque bearer token lives in SSM, below).
const forgejoBaseURL = "https://forgejo.coilysiren.me"

// forgejoSSMTokenPath is the SSM path holding the bearer API token: the
// `coilyco-ops` bot, not a human PAT (ward#160). See docs/agent.md.
const forgejoSSMTokenPath = "/forgejo/coilyco-ops/api-token" //nolint:gosec // SSM path, not a credential

// forgejoAPIHTTPTimeout caps each Forgejo API call so a stalled DNS or hung
// connection fails fast.
const forgejoAPIHTTPTimeout = 20 * time.Second

// forgejoAPIToken fetches the bearer token from SSM via the audited shell
// runner (aws ssm get-parameter --with-decryption), mirroring coily.
func (r *Runner) forgejoAPIToken(ctx context.Context) (string, error) {
	out, err := r.Runner.Capture(ctx, "aws",
		"ssm", "get-parameter",
		"--name", forgejoSSMTokenPath,
		"--with-decryption",
		"--query", "Parameter.Value",
		"--output", "text",
	)
	if err != nil {
		return "", fmt.Errorf("dispatch forgejo: fetch %s: %w", forgejoSSMTokenPath, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("dispatch forgejo: %s resolved to empty value", forgejoSSMTokenPath)
	}
	return v, nil
}

// fetchForgejoIssue GETs a Forgejo issue and wraps it into dispatch.Issue. A
// 404-shaped error lets the dispatch package fall back to GitHub.
func (r *Runner) fetchForgejoIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	token, err := r.forgejoAPIToken(ctx)
	if err != nil {
		return nil, err
	}
	target := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d",
		strings.TrimSuffix(forgejoBaseURL, "/"), owner, repo, number)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("dispatch forgejo: build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: forgejoAPIHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dispatch forgejo: GET %s: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dispatch forgejo: GET %s returned HTTP %d: %s",
			target, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var issue dispatch.Issue
	if err := json.Unmarshal(respBody, &issue); err != nil {
		return nil, fmt.Errorf("dispatch forgejo: parse %s: %w", target, err)
	}
	return &issue, nil
}

// forgejoClient is a minimal write-capable client built from an explicit base +
// token, so it works inside a container ($FORGEJO_TOKEN, no SSM). Reaper's path.
type forgejoClient struct {
	base   string
	token  string
	client *http.Client
	// mode is the agent identity stamped onto every write body (ward#155);
	// defaults to the container env, or pinned by host callers via withMode.
	mode containerMode
}

func newForgejoClient(base, token string) *forgejoClient {
	return &forgejoClient{
		base:   strings.TrimRight(base, "/"),
		token:  token,
		client: &http.Client{Timeout: forgejoAPIHTTPTimeout},
		mode:   currentAgentMode(),
	}
}

// withMode pins the signing identity for host-side callers that know the mode
// rather than inheriting it from the container env. Returns the client.
func (c *forgejoClient) withMode(m containerMode) *forgejoClient {
	c.mode = m
	return c
}

// do issues one authenticated JSON request and returns the status + raw body.
func (c *forgejoClient) do(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("forgejo: marshal %s body: %w", path, err)
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("forgejo: build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("forgejo: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

// createIssue opens a new issue and returns its number. The body is signed with
// the agent attribution (ward#155) before it is sent.
func (c *forgejoClient) createIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo)
	status, respBody, err := c.do(ctx, http.MethodPost, path, map[string]string{"title": title, "body": c.mode.signBody(body)})
	if err != nil {
		return 0, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return 0, fmt.Errorf("forgejo: create issue returned HTTP %d: %s", status, strings.TrimSpace(string(respBody)))
	}
	var out struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return 0, fmt.Errorf("forgejo: parse created issue: %w", err)
	}
	return out.Number, nil
}

// commentIssue appends a comment to an existing issue. The body is signed with
// the agent attribution (ward#155) before it is sent.
func (c *forgejoClient) commentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, number)
	status, respBody, err := c.do(ctx, http.MethodPost, path, map[string]string{"body": c.mode.signBody(body)})
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("forgejo: comment issue #%d returned HTTP %d: %s", number, status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// issueComment is one row of an issue's comment thread - just the fields the
// reservation check needs: body (for the marker), author, and post time.
type issueComment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// listIssueComments fetches an issue's comment thread, oldest first.
func (c *forgejoClient) listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, number)
	status, respBody, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("forgejo: list comments on #%d returned HTTP %d: %s", number, status, strings.TrimSpace(string(respBody)))
	}
	var out []issueComment
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("forgejo: parse comments on #%d: %w", number, err)
	}
	return out, nil
}

// leanIssueView is the trimmed `issue view` payload: issue + comments, every
// user a login literal (ward#225). See docs/ops-forgejo-view.md.
type leanIssueView struct {
	Issue    leanIssue     `json:"issue"`
	Comments []leanComment `json:"comments"`
}

// leanIssue is the issue itself, with user/assignees/labels reduced to the
// scalar names a reader scans for - never the nested profile objects.
type leanIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	User      string    `json:"user"`
	Labels    []string  `json:"labels,omitempty"`
	Assignees []string  `json:"assignees,omitempty"`
	Comments  int       `json:"comments"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
	Body      string    `json:"body"`
}

// leanComment is one comment row: author login, post time, body. No profile.
type leanComment struct {
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"body"`
}

// forgejoIssueRaw is the issue subset the lean view keeps; unmarshalling into
// these typed fields IS the projection - unnamed profile fields are dropped.
type forgejoIssueRaw struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	Comments  int       `json:"comments"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// lean collapses the raw issue into its scalar-name projection.
func (raw forgejoIssueRaw) lean() leanIssue {
	li := leanIssue{
		Number:    raw.Number,
		Title:     raw.Title,
		State:     raw.State,
		User:      raw.User.Login,
		Comments:  raw.Comments,
		CreatedAt: raw.CreatedAt,
		UpdatedAt: raw.UpdatedAt,
		HTMLURL:   raw.HTMLURL,
		Body:      raw.Body,
	}
	for _, l := range raw.Labels {
		li.Labels = append(li.Labels, l.Name)
	}
	for _, a := range raw.Assignees {
		li.Assignees = append(li.Assignees, a.Login)
	}
	return li
}

// viewIssue fetches an issue and its comment thread, projected to the lean shape
// so a reader gets usernames, not a full profile repeated per comment (ward#225).
func (c *forgejoClient) viewIssue(ctx context.Context, owner, repo string, number int) (*leanIssueView, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d", owner, repo, number)
	status, respBody, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("forgejo: view issue #%d returned HTTP %d: %s", number, status, strings.TrimSpace(string(respBody)))
	}
	var raw forgejoIssueRaw
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: parse issue #%d: %w", number, err)
	}
	comments, err := c.listIssueComments(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	view := &leanIssueView{Issue: raw.lean(), Comments: make([]leanComment, 0, len(comments))}
	for _, cm := range comments {
		view.Comments = append(view.Comments, leanComment{
			User:      cm.User.Login,
			CreatedAt: cm.CreatedAt,
			Body:      cm.Body,
		})
	}
	return view, nil
}

// closeIssue flips an existing issue to the closed state (PATCH state=closed),
// used by the task route flow to retire an intake record once it's cross-linked.
func (c *forgejoClient) closeIssue(ctx context.Context, owner, repo string, number int) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d", owner, repo, number)
	status, respBody, err := c.do(ctx, http.MethodPatch, path, map[string]string{"state": "closed"})
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("forgejo: close issue #%d returned HTTP %d: %s", number, status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// repoBrief is one row of an owner's repo list - just the fields the task route
// survey needs to build a catalog the agent picks from (ward#164).
type repoBrief struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	Empty       bool   `json:"empty"`
}

// listOwnerRepos lists an owner's repos, trying the org endpoint then the user
// endpoint (a Forgejo org is a kind of user); a missing owner returns no repos.
func (c *forgejoClient) listOwnerRepos(ctx context.Context, owner string) ([]repoBrief, error) {
	for _, path := range []string{
		fmt.Sprintf("/api/v1/orgs/%s/repos?limit=100", owner),
		fmt.Sprintf("/api/v1/users/%s/repos?limit=100", owner),
	} {
		status, respBody, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			continue
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("forgejo: list repos for %s returned HTTP %d: %s", owner, status, strings.TrimSpace(string(respBody)))
		}
		var out []repoBrief
		if err := json.Unmarshal(respBody, &out); err != nil {
			return nil, fmt.Errorf("forgejo: parse repos for %s: %w", owner, err)
		}
		return out, nil
	}
	return nil, nil
}

// findOpenIssueByTitlePrefix returns the first open issue whose title starts
// with prefix, so the reaper appends instead of filing a duplicate.
func (c *forgejoClient) findOpenIssueByTitlePrefix(ctx context.Context, owner, repo, prefix string) (number int, found bool, err error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues?state=open&type=issues&limit=50", owner, repo)
	status, respBody, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, false, err
	}
	if status != http.StatusOK {
		return 0, false, fmt.Errorf("forgejo: list issues returned HTTP %d: %s", status, strings.TrimSpace(string(respBody)))
	}
	var issues []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(respBody, &issues); err != nil {
		return 0, false, fmt.Errorf("forgejo: parse issue list: %w", err)
	}
	for _, i := range issues {
		if strings.HasPrefix(i.Title, prefix) {
			return i.Number, true, nil
		}
	}
	return 0, false, nil
}
