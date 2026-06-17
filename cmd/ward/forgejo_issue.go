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
// for `ward dispatch` to resolve a Forgejo issue ref. See docs/dispatch.md.

// forgejoBaseURL is the Forgejo origin. A meaningful host, safe to hardcode
// (the opaque bearer token lives in SSM, below).
const forgejoBaseURL = "https://forgejo.coilysiren.me"

// forgejoSSMTokenPath is the SSM path holding the bearer API token. A
// meaningful path, not a credential; fetched with --with-decryption at use.
const forgejoSSMTokenPath = "/forgejo/api-token" //nolint:gosec // SSM path, not a credential

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
}

func newForgejoClient(base, token string) *forgejoClient {
	return &forgejoClient{
		base:   strings.TrimRight(base, "/"),
		token:  token,
		client: &http.Client{Timeout: forgejoAPIHTTPTimeout},
	}
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

// createIssue opens a new issue and returns its number.
func (c *forgejoClient) createIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo)
	status, respBody, err := c.do(ctx, http.MethodPost, path, map[string]string{"title": title, "body": body})
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

// commentIssue appends a comment to an existing issue.
func (c *forgejoClient) commentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, number)
	status, respBody, err := c.do(ctx, http.MethodPost, path, map[string]string{"body": body})
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("forgejo: comment issue #%d returned HTTP %d: %s", number, status, strings.TrimSpace(string(respBody)))
	}
	return nil
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
