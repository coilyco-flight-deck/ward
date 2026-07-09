package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// agent_director_prmerge.go defines the narrow PR-merge authority the director
// may exercise for ward-owned `pull-requests-and-merge` runs.

const directorPRMergeMarker = "ward.workflow: pull-requests-and-merge"

type forgejoPullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	Draft   bool   `json:"draft"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

func (r *Runner) directorForgejoClient(ctx context.Context) (*forgejoClient, error) {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return nil, err
	}
	tok, err := r.forgejoTokenResolver(ctx, forgejoTokenSSMPath)
	if err != nil {
		return nil, err
	}
	cl = cl.withToken(strings.TrimSpace(tok))
	return cl, nil
}

func (c *forgejoClient) apiJSON(ctx context.Context, method, endpoint string, payload []byte) ([]byte, int, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, 0, fmt.Errorf("no Forgejo token available")
	}
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, resp.StatusCode, fmt.Errorf("forgejo %s %s returned %s: %s", method, endpoint, resp.Status, firstLine(string(body)))
	}
	return body, resp.StatusCode, nil
}

func (c *forgejoClient) listOpenPullRequests(ctx context.Context, owner, repo string) ([]forgejoPullRequest, error) {
	var out []forgejoPullRequest
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("/api/v1/repos/%s/%s/pulls?state=open&limit=50&page=%d",
			url.PathEscape(owner), url.PathEscape(repo), page)
		body, _, err := c.apiJSON(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("forgejo: list pull requests in %s/%s: %w", owner, repo, err)
		}
		var pageItems []forgejoPullRequest
		if err := json.Unmarshal(body, &pageItems); err != nil {
			return nil, fmt.Errorf("forgejo: parse pull requests in %s/%s: %w", owner, repo, err)
		}
		out = append(out, pageItems...)
		if len(pageItems) < 50 {
			break
		}
	}
	return out, nil
}

func (c *forgejoClient) mergePullRequest(ctx context.Context, owner, repo string, index int) error {
	payload, err := json.Marshal(map[string]string{"Do": "merge"})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/merge",
		url.PathEscape(owner), url.PathEscape(repo), index)
	_, _, err = c.apiJSON(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return fmt.Errorf("forgejo: merge %s/%s#%d: %w", owner, repo, index, err)
	}
	return nil
}

func (r *Runner) directorMergeEligiblePullRequests(ctx context.Context, label string, repos []string) error {
	cl, err := r.directorForgejoClient(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	entries := r.backlogScopeEntries(repos)
	ledger := make(map[string]*backlogEntry, len(entries))
	for _, e := range entries {
		ledger[mergeLedgerKey(e.repo, e.Num)] = e
	}
	for _, repo := range repos {
		owner, name, _ := strings.Cut(repo, "/")
		prs, lerr := cl.listOpenPullRequests(ctx, owner, name)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "%s: note: cannot list pull requests in %s (%v); skipping this repo\n", label, repo, lerr)
			continue
		}
		for _, pr := range prs {
			issueNum, eligible, reason := directorMergeEligibility(repo, pr, ledger[mergeLedgerKey(repo, prIssueNumber(pr))])
			if !eligible {
				if reason != "" {
					fmt.Fprintf(os.Stderr, "%s: not merging %s/%s#%d - %s\n", label, owner, name, pr.Number, reason)
				}
				continue
			}
			if err := cl.mergePullRequest(ctx, owner, name, pr.Number); err != nil {
				fmt.Fprintf(os.Stderr, "%s: merge failed for %s/%s#%d (issue #%d): %v\n", label, owner, name, pr.Number, issueNum, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "%s: merged eligible PR %s/%s#%d for issue #%d\n", label, owner, name, pr.Number, issueNum)
		}
	}
	return nil
}

func mergeLedgerKey(repo string, num int) string {
	return repo + "#" + strconv.Itoa(num)
}

func prIssueNumber(pr forgejoPullRequest) int {
	nums := parseIssueNumbers(pr.Body)
	if len(nums) == 0 {
		return 0
	}
	return nums[0]
}

func directorMergeEligibility(repo string, pr forgejoPullRequest, entry *backlogEntry) (issueNum int, eligible bool, reason string) {
	if strings.TrimSpace(pr.State) != "" && !strings.EqualFold(pr.State, "open") {
		return 0, false, "pull request is not open"
	}
	if pr.Draft {
		return 0, false, "pull request is draft"
	}
	if strings.HasPrefix(strings.TrimSpace(pr.Head.Ref), "ward-salvage/") {
		return 0, false, "salvage PRs stay out of the merge policy"
	}
	if !strings.Contains(strings.ToLower(pr.Body), directorPRMergeMarker) {
		return 0, false, "missing the director merge marker"
	}
	issueNum = prIssueNumber(pr)
	if issueNum == 0 {
		return 0, false, "missing a closing issue reference"
	}
	if !strings.EqualFold(strings.TrimSpace(pr.Head.Ref), fmt.Sprintf("issue-%d", issueNum)) {
		return issueNum, false, "head branch is not the ward issue branch"
	}
	if entry == nil {
		return issueNum, false, "matching issue ledger entry not found"
	}
	if entry.State != "done" || entry.LastOutcome == nil || entry.LastOutcome.Status != "done" {
		return issueNum, false, "linked issue is not done"
	}
	if !strings.Contains(strings.ToLower(entry.LastOutcome.Text), "review summary: passed") {
		return issueNum, false, "review gate did not pass"
	}
	return issueNum, true, ""
}
