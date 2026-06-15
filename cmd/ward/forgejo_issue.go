package main

import (
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
