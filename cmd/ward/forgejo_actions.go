package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// forgejo_actions.go extends ward's core Forgejo client with the Actions run
// surface (ward#1067): list, per-run conclusion, rerun. docs/agent-pr-workflow.md.

// forgejoActionRun is one row of the repo Actions run feed - the fields a
// red/green + rerun decision reads.
type forgejoActionRun struct {
	ID          int64     `json:"id"`
	IndexInRepo int64     `json:"index_in_repo"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	WorkflowID  string    `json:"workflow_id"`
	PrettyRef   string    `json:"prettyref"`
	CommitSHA   string    `json:"commit_sha"`
	Event       string    `json:"event"`
	HTMLURL     string    `json:"html_url"`
	Started     time.Time `json:"started"`
	Stopped     time.Time `json:"stopped"`
}

// listActionRuns reads a repo's Actions runs, newest first; Status doubles as
// the conclusion (success/failure/cancelled/skipped) once a run is done.
func (c *forgejoClient) listActionRuns(ctx context.Context, owner, repo string, limit int) ([]forgejoActionRun, error) {
	if limit <= 0 {
		limit = 20
	}
	// Forgejo ignores `limit` on this endpoint unless `page` rides along.
	q := url.Values{"page": {"1"}, "limit": {strconv.Itoa(limit)}}
	var feed struct {
		WorkflowRuns []forgejoActionRun `json:"workflow_runs"`
	}
	if _, err := c.doJSON(ctx, http.MethodGet, []string{"repos", owner, repo, "actions", "runs"}, q, nil, true, &feed); err != nil {
		return nil, fmt.Errorf("forgejo: list action runs in %s/%s: %w", owner, repo, err)
	}
	return feed.WorkflowRuns, nil
}

// getActionRun reads one Actions run by its id (GET
// /repos/{owner}/{repo}/actions/runs/{run_id}) for a per-run conclusion.
func (c *forgejoClient) getActionRun(ctx context.Context, owner, repo string, runID int64) (*forgejoActionRun, error) {
	var run forgejoActionRun
	if _, err := c.doJSON(ctx, http.MethodGet, []string{"repos", owner, repo, "actions", "runs", strconv.FormatInt(runID, 10)}, nil, nil, true, &run); err != nil {
		return nil, fmt.Errorf("forgejo: get action run %s/%s run %d: %w", owner, repo, runID, err)
	}
	return &run, nil
}

// errForgeRerunUnsupported marks the agentic-os#434 forge gap: the Forgejo
// REST API exposes no rerun operation, so ward's native tool degrades loudly.
var errForgeRerunUnsupported = fmt.Errorf("the Forgejo REST API on this forge exposes no Actions rerun operation (agentic-os#434)")

// rerunActionRun asks the forge to rerun one Actions run; 404/405 degrades to
// errForgeRerunUnsupported with the manual fallback. docs/agent-pr-workflow.md.
func (c *forgejoClient) rerunActionRun(ctx context.Context, owner, repo string, runID int64) error {
	_, err := c.doJSON(ctx, http.MethodPost, []string{"repos", owner, repo, "actions", "runs", strconv.FormatInt(runID, 10), "rerun"}, nil, nil, true, nil)
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "404") || strings.Contains(msg, "405") {
		return fmt.Errorf("forgejo: rerun action run %s/%s run %d: %w; retrigger it by pushing to the run's branch (an empty commit works) or wait for the forge to ship the rerun API", owner, repo, runID, errForgeRerunUnsupported)
	}
	return fmt.Errorf("forgejo: rerun action run %s/%s run %d: %w", owner, repo, runID, err)
}

// pullRequestMerged is the merged-state check the native merge tool confirms
// with: GET /repos/{owner}/{repo}/pulls/{index}/merge, 204 merged / 404 not.
func (c *forgejoClient) pullRequestMerged(ctx context.Context, owner, repo string, index int) (bool, error) {
	token, err := c.apiToken(ctx)
	if err != nil {
		return false, err
	}
	endpoint := c.apiURL("repos", owner, repo, "pulls", strconv.Itoa(index), "merge")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "token "+token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("forgejo: merged-state check for %s/%s#%d returned %s", owner, repo, index, resp.Status)
	}
}
