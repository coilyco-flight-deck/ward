package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
)

// shortcut_ops.go is ward's Shortcut tracker client, using the public REST API
// directly with the user-provided API token in the environment.

type shortcutClient struct {
	r       *Runner
	mode    containerMode
	baseURL string
	token   string
}

func (r *Runner) hostShortcutClient(mode containerMode) (*shortcutClient, error) {
	tok := strings.TrimSpace(os.Getenv(shortcutTokenEnv))
	if tok == "" {
		return nil, fmt.Errorf("shortcut: no API token found - set %s to a Shortcut API token", shortcutTokenEnv)
	}
	return &shortcutClient{r: r, mode: mode, baseURL: shortcutAPIBaseURL, token: tok}, nil
}

func (c *shortcutClient) apiURL(path string) string {
	return strings.TrimRight(c.baseURL, "/") + path
}

func (c *shortcutClient) request(ctx context.Context, method, path string, in any, out any, wantStatus int) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL(path), body)
	if err != nil {
		return err
	}
	req.Header.Set("Shortcut-Token", c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return readErr
	}
	if wantStatus != 0 && resp.StatusCode != wantStatus {
		return fmt.Errorf("shortcut %s %s returned %s after %d byte(s): %s", method, path, resp.Status, len(data), responseSnippet(data))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("shortcut %s %s parse %s after %d byte(s): %w", method, path, responseSnippet(data), len(data), err)
		}
	}
	return nil
}

type shortcutStoryRaw struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	AppURL          string `json:"app_url"`
	WorkflowID      int    `json:"workflow_id"`
	WorkflowStateID int    `json:"workflow_state_id"`
	Completed       bool   `json:"completed"`
	Labels          []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type shortcutStoryCommentRaw struct {
	ID        int    `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	AuthorID  string `json:"author_id"`
}

type shortcutWorkflowRaw struct {
	ID             int                        `json:"id"`
	DefaultStateID int                        `json:"default_state_id"`
	States         []shortcutWorkflowStateRaw `json:"states"`
}

type shortcutWorkflowStateRaw struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (c *shortcutClient) getIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	var raw shortcutStoryRaw
	if err := c.request(ctx, http.MethodGet, "/stories/"+strconv.Itoa(number), nil, &raw, http.StatusOK); err != nil {
		return nil, fmt.Errorf("shortcut: get story %s/%s#%d: %w", owner, repo, number, err)
	}
	issue := &dispatch.Issue{
		Number: raw.ID,
		Title:  raw.Name,
		Body:   raw.Description,
		URL:    raw.AppURL,
	}
	if raw.Completed {
		issue.State = "closed"
	} else {
		issue.State = "open"
	}
	for _, l := range raw.Labels {
		if l.Name != "" {
			issue.Labels = append(issue.Labels, l.Name)
		}
	}
	return issue, nil
}

func (c *shortcutClient) listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	var raw []shortcutStoryCommentRaw
	if err := c.request(ctx, http.MethodGet, "/stories/"+strconv.Itoa(number)+"/comments", nil, &raw, http.StatusOK); err != nil {
		return nil, fmt.Errorf("shortcut: list comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	comments := make([]issueComment, 0, len(raw))
	for _, rc := range raw {
		ic := issueComment{Body: rc.Text}
		ic.User.Login = rc.AuthorID
		if t, err := time.Parse(time.RFC3339, rc.CreatedAt); err == nil {
			ic.CreatedAt = t
		}
		comments = append(comments, ic)
	}
	return comments, nil
}

func (c *shortcutClient) createIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
	stateID, err := c.shortcutCreateStateID(ctx)
	if err != nil {
		return 0, err
	}
	var raw struct {
		ID int `json:"id"`
	}
	req := map[string]any{
		"name":              title,
		"description":       c.mode.signBody(body),
		"workflow_state_id": stateID,
	}
	if labels := shortcutCreateLabels(); len(labels) > 0 {
		req["labels"] = labels
	}
	if epicID, ok := shortcutCreateEpicID(); ok {
		req["epic_id"] = epicID
	}
	if err := c.request(ctx, http.MethodPost, "/stories", req, &raw, http.StatusCreated); err != nil {
		return 0, fmt.Errorf("shortcut: create story in %s/%s: %w", owner, repo, err)
	}
	return raw.ID, nil
}

func (c *shortcutClient) commentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	req := map[string]any{"text": c.mode.signBody(body)}
	if err := c.request(ctx, http.MethodPost, "/stories/"+strconv.Itoa(number)+"/comments", req, nil, http.StatusCreated); err != nil {
		return fmt.Errorf("shortcut: comment story %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

func (c *shortcutClient) closeIssue(ctx context.Context, owner, repo string, number int) error {
	stateID, err := c.shortcutStateIDForStory(ctx, number, "done")
	if err != nil {
		return err
	}
	if err := c.request(ctx, http.MethodPut, "/stories/"+strconv.Itoa(number), map[string]any{"workflow_state_id": stateID}, nil, http.StatusOK); err != nil {
		return fmt.Errorf("shortcut: close story %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

func (c *shortcutClient) reopenIssue(ctx context.Context, owner, repo string, number int) error {
	stateID, err := c.shortcutStateIDForStory(ctx, number, "default")
	if err != nil {
		return err
	}
	if err := c.request(ctx, http.MethodPut, "/stories/"+strconv.Itoa(number), map[string]any{"workflow_state_id": stateID}, nil, http.StatusOK); err != nil {
		return fmt.Errorf("shortcut: reopen story %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

func (c *shortcutClient) lockIssue(context.Context, string, string, int) error {
	return errForgeLockUnsupported
}

func (c *shortcutClient) unlockIssue(context.Context, string, string, int) error {
	return errForgeLockUnsupported
}

func shortcutCreateLabels() []map[string]string {
	raw := strings.TrimSpace(os.Getenv("SHORTCUT_LABELS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]map[string]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, map[string]string{"name": name})
		}
	}
	return out
}

func shortcutCreateEpicID() (int, bool) {
	raw := strings.TrimSpace(os.Getenv("SHORTCUT_EPIC_ID"))
	if raw == "" {
		return 0, false
	}
	n, err := parsePositiveInt(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func (c *shortcutClient) shortcutCreateStateID(ctx context.Context) (int, error) {
	if raw := strings.TrimSpace(os.Getenv("SHORTCUT_WORKFLOW_STATE_ID")); raw != "" {
		n, err := parsePositiveInt(raw)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("shortcut: invalid SHORTCUT_WORKFLOW_STATE_ID %q", raw)
		}
		return n, nil
	}
	workflows, err := c.listWorkflows(ctx)
	if err != nil {
		return 0, err
	}
	for _, wf := range workflows {
		if wf.DefaultStateID != 0 {
			return wf.DefaultStateID, nil
		}
		for _, st := range wf.States {
			if strings.EqualFold(st.Type, "unstarted") || strings.EqualFold(st.Type, "backlog") {
				return st.ID, nil
			}
		}
	}
	return 0, fmt.Errorf("shortcut: no workflow state available for story creation")
}

func (c *shortcutClient) shortcutStateIDForStory(ctx context.Context, storyID int, want string) (int, error) {
	story, err := c.shortcutStory(ctx, storyID)
	if err != nil {
		return 0, err
	}
	workflows, err := c.listWorkflows(ctx)
	if err != nil {
		return 0, err
	}
	for _, wf := range workflows {
		if wf.ID != story.WorkflowID {
			continue
		}
		switch want {
		case "done":
			for _, st := range wf.States {
				if strings.EqualFold(st.Type, "done") {
					return st.ID, nil
				}
			}
		default:
			if wf.DefaultStateID != 0 {
				return wf.DefaultStateID, nil
			}
			for _, st := range wf.States {
				if strings.EqualFold(st.Type, "unstarted") || strings.EqualFold(st.Type, "backlog") {
					return st.ID, nil
				}
			}
		}
		return 0, fmt.Errorf("shortcut: workflow %d has no %s state", wf.ID, want)
	}
	return 0, fmt.Errorf("shortcut: story %d references unknown workflow %d", storyID, story.WorkflowID)
}

func (c *shortcutClient) shortcutStory(ctx context.Context, storyID int) (*shortcutStoryRaw, error) {
	var raw shortcutStoryRaw
	if err := c.request(ctx, http.MethodGet, "/stories/"+strconv.Itoa(storyID), nil, &raw, http.StatusOK); err != nil {
		return nil, err
	}
	return &raw, nil
}

func (c *shortcutClient) listWorkflows(ctx context.Context) ([]shortcutWorkflowRaw, error) {
	var raw []shortcutWorkflowRaw
	if err := c.request(ctx, http.MethodGet, "/workflows", nil, &raw, http.StatusOK); err != nil {
		return nil, fmt.Errorf("shortcut: list workflows: %w", err)
	}
	return raw, nil
}
