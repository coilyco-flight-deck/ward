package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// gitlab_ops.go is ward's GitLab issue-thread + merge-request client. It mirrors
// the GitHub/Forgejo adapters with GitLab's native REST API and a configurable base.

type gitlabClient struct {
	r       *Runner
	mode    containerMode
	baseURL string
	token   string
}

func (r *Runner) hostGitLabClient(_ context.Context, mode containerMode) *gitlabClient {
	return &gitlabClient{r: r, mode: mode, baseURL: gitlabBaseURL()}
}

func (c *gitlabClient) apiToken(ctx context.Context, owner, repo string) (string, error) {
	if tok := strings.TrimSpace(c.token); tok != "" {
		return tok, nil
	}
	if c.r == nil {
		return "", fmt.Errorf("no GitLab token available")
	}
	tok, err := c.r.resolveGitLabToken(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	c.token = tok
	return tok, nil
}

func (c *gitlabClient) apiBase() string {
	base := strings.TrimRight(c.baseURL, "/")
	if base == "" {
		base = gitlabBaseURL()
	}
	return base + "/api/v4"
}

func gitlabProjectPath(owner, repo string) string {
	return "/projects/" + url.PathEscape(owner+"/"+repo)
}

func (c *gitlabClient) do(ctx context.Context, owner, repo, method, path string, body []byte) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase()+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	token, err := c.apiToken(ctx, owner, repo)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	data, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return resp, nil, readErr
	}
	return resp, data, nil
}

func (c *gitlabClient) getIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	resp, data, err := c.do(ctx, owner, repo, http.MethodGet, gitlabProjectPath(owner, repo)+"/issues/"+strconv.Itoa(number), nil) //nolint:bodyclose
	if err != nil {
		return nil, fmt.Errorf("gitlab: get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab: get issue %s/%s#%d returned %s: %s", owner, repo, number, resp.Status, firstLine(string(data)))
	}
	var raw struct {
		IID         int      `json:"iid"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		State       string   `json:"state"`
		WebURL      string   `json:"web_url"`
		Labels      []string `json:"labels"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gitlab: parse issue %s/%s#%d: %w", owner, repo, number, err)
	}
	issue := &Issue{
		Number: raw.IID,
		Title:  raw.Title,
		Body:   raw.Description,
		State:  strings.ToLower(raw.State),
		URL:    raw.WebURL,
		Labels: append([]string(nil), raw.Labels...),
	}
	return issue, nil
}

func (c *gitlabClient) listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	path := gitlabProjectPath(owner, repo) + "/issues/" + strconv.Itoa(number) + "/notes?sort=asc&order_by=created_at"
	resp, data, err := c.do(ctx, owner, repo, http.MethodGet, path, nil) //nolint:bodyclose
	if err != nil {
		return nil, fmt.Errorf("gitlab: list comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab: list comments on %s/%s#%d returned %s: %s", owner, repo, number, resp.Status, firstLine(string(data)))
	}
	var raw []struct {
		ID        int    `json:"id"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		Author    struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gitlab: parse comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	out := make([]issueComment, 0, len(raw))
	for _, rc := range raw {
		ic := issueComment{ID: rc.ID, Body: rc.Body}
		ic.User.Login = rc.Author.Username
		if t, err := time.Parse(time.RFC3339Nano, rc.CreatedAt); err == nil {
			ic.CreatedAt = t
		} else if t, err := time.Parse(time.RFC3339, rc.CreatedAt); err == nil {
			ic.CreatedAt = t
		}
		out = append(out, ic)
	}
	return out, nil
}

func (c *gitlabClient) listPullRequestComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	path := gitlabProjectPath(owner, repo) + "/merge_requests/" + strconv.Itoa(number) + "/notes?sort=asc&order_by=created_at"
	resp, data, err := c.do(ctx, owner, repo, http.MethodGet, path, nil) //nolint:bodyclose
	if err != nil {
		return nil, fmt.Errorf("gitlab: list comments on %s/%s!%d: %w", owner, repo, number, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab: list comments on %s/%s!%d returned %s: %s", owner, repo, number, resp.Status, firstLine(string(data)))
	}
	var raw []struct {
		ID        int    `json:"id"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		Author    struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gitlab: parse comments on %s/%s!%d: %w", owner, repo, number, err)
	}
	out := make([]issueComment, 0, len(raw))
	for _, rc := range raw {
		ic := issueComment{ID: rc.ID, Body: rc.Body}
		ic.User.Login = rc.Author.Username
		if t, err := time.Parse(time.RFC3339Nano, rc.CreatedAt); err == nil {
			ic.CreatedAt = t
		} else if t, err := time.Parse(time.RFC3339, rc.CreatedAt); err == nil {
			ic.CreatedAt = t
		}
		out = append(out, ic)
	}
	return out, nil
}

func (c *gitlabClient) getPullRequestContext(ctx context.Context, owner, repo string, number int) (*agentPullRequestContext, error) {
	resp, data, err := c.do(ctx, owner, repo, http.MethodGet, gitlabProjectPath(owner, repo)+"/merge_requests/"+strconv.Itoa(number), nil) //nolint:bodyclose
	if err != nil {
		return nil, fmt.Errorf("gitlab: get merge request %s/%s!%d: %w", owner, repo, number, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab: get merge request %s/%s!%d returned %s: %s", owner, repo, number, resp.Status, firstLine(string(data)))
	}
	var raw struct {
		IID                 int    `json:"iid"`
		Title               string `json:"title"`
		Description         string `json:"description"`
		State               string `json:"state"`
		WebURL              string `json:"web_url"`
		SourceBranch        string `json:"source_branch"`
		TargetBranch        string `json:"target_branch"`
		MergeStatus         string `json:"merge_status"`
		DetailedMergeStatus string `json:"detailed_merge_status"`
		WorkInProgress      bool   `json:"work_in_progress"`
		Draft               bool   `json:"draft"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gitlab: parse merge request %s/%s!%d: %w", owner, repo, number, err)
	}
	mergeability := strings.TrimSpace(raw.DetailedMergeStatus)
	if mergeability == "" {
		mergeability = strings.TrimSpace(raw.MergeStatus)
	}
	if mergeability == "" {
		mergeability = "unknown"
	}
	if raw.WorkInProgress || raw.Draft {
		mergeability += ", draft=true"
	}
	return &agentPullRequestContext{
		State:        normalizeOpenState(raw.State),
		Title:        strings.TrimSpace(raw.Title),
		Body:         strings.TrimSpace(raw.Description),
		URL:          strings.TrimSpace(raw.WebURL),
		HeadRef:      strings.TrimSpace(raw.SourceBranch),
		BaseRef:      strings.TrimSpace(raw.TargetBranch),
		Mergeability: mergeability,
	}, nil
}

func normalizeOpenState(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "opened":
		return "open"
	default:
		return strings.TrimSpace(s)
	}
}

func (c *gitlabClient) createIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
	payload, err := json.Marshal(map[string]string{
		"title":       title,
		"description": c.mode.signBody(body),
	})
	if err != nil {
		return 0, err
	}
	resp, data, err := c.do(ctx, owner, repo, http.MethodPost, gitlabProjectPath(owner, repo)+"/issues", payload) //nolint:bodyclose
	if err != nil {
		return 0, fmt.Errorf("gitlab: create issue in %s/%s: %w", owner, repo, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("gitlab: create issue in %s/%s returned %s: %s", owner, repo, resp.Status, firstLine(string(data)))
	}
	var created struct {
		IID int `json:"iid"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return 0, fmt.Errorf("gitlab: parse created issue in %s/%s: %w", owner, repo, err)
	}
	return created.IID, nil
}

func (c *gitlabClient) commentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	payload, err := json.Marshal(map[string]string{"body": c.mode.signBody(body)})
	if err != nil {
		return err
	}
	resp, data, err := c.do(ctx, owner, repo, http.MethodPost, gitlabProjectPath(owner, repo)+"/issues/"+strconv.Itoa(number)+"/notes", payload) //nolint:bodyclose
	if err != nil {
		return fmt.Errorf("gitlab: comment issue %s/%s#%d: %w", owner, repo, number, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitlab: comment issue %s/%s#%d returned %s: %s", owner, repo, number, resp.Status, firstLine(string(data)))
	}
	return nil
}

func (c *gitlabClient) deleteIssueComment(_ context.Context, _, _ string, _ int) error {
	// The shared Tracker surface does not carry the parent issue IID here, so the
	// GitLab adapter leaves comment deletion best-effort.
	return nil
}

func (c *gitlabClient) closeIssue(ctx context.Context, owner, repo string, number int) error {
	resp, data, err := c.do(ctx, owner, repo, http.MethodPut, gitlabProjectPath(owner, repo)+"/issues/"+strconv.Itoa(number)+"?state_event=close", nil) //nolint:bodyclose
	if err != nil {
		return fmt.Errorf("gitlab: close issue %s/%s#%d: %w", owner, repo, number, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitlab: close issue %s/%s#%d returned %s: %s", owner, repo, number, resp.Status, firstLine(string(data)))
	}
	return nil
}

func (c *gitlabClient) reopenIssue(ctx context.Context, owner, repo string, number int) error {
	resp, data, err := c.do(ctx, owner, repo, http.MethodPut, gitlabProjectPath(owner, repo)+"/issues/"+strconv.Itoa(number)+"?state_event=reopen", nil) //nolint:bodyclose
	if err != nil {
		return fmt.Errorf("gitlab: reopen issue %s/%s#%d: %w", owner, repo, number, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitlab: reopen issue %s/%s#%d returned %s: %s", owner, repo, number, resp.Status, firstLine(string(data)))
	}
	return nil
}

func (c *gitlabClient) lockIssue(context.Context, string, string, int) error {
	return errForgeLockUnsupported
}

func (c *gitlabClient) unlockIssue(context.Context, string, string, int) error {
	return errForgeLockUnsupported
}

func (c *gitlabClient) repoPullRequestsEnabled(context.Context, string, string) (bool, error) {
	return true, nil
}

func (c *gitlabClient) createPullRequest(ctx context.Context, owner, repo, head, base, title, body string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"source_branch": head,
		"target_branch": base,
		"title":         title,
		"description":   c.mode.signBody(body),
	})
	if err != nil {
		return "", err
	}
	resp, data, err := c.do(ctx, owner, repo, http.MethodPost, gitlabProjectPath(owner, repo)+"/merge_requests", payload) //nolint:bodyclose
	if err != nil {
		return "", fmt.Errorf("gitlab: create merge request in %s/%s: %w", owner, repo, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gitlab: create merge request in %s/%s returned %s: %s", owner, repo, resp.Status, firstLine(string(data)))
	}
	var created struct {
		WebURL string `json:"web_url"`
		IID    int    `json:"iid"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return "", fmt.Errorf("gitlab: parse created merge request in %s/%s: %w", owner, repo, err)
	}
	if created.WebURL != "" {
		return created.WebURL, nil
	}
	if created.IID != 0 {
		return strings.TrimRight(c.baseURL, "/") + "/" + owner + "/" + repo + "/-/merge_requests/" + strconv.Itoa(created.IID), nil
	}
	return "", fmt.Errorf("gitlab: create merge request response omitted web_url")
}
