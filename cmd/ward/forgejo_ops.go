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

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

// forgejo_ops.go is ward's core Forgejo client. Core agent paths use this
// direct HTTP adapter, not the runtime-selected `ward ops forgejo` KDL surface.

// forgejoBaseURL is the Forgejo origin, used to render issue URLs and parse refs.
var forgejoBaseURL = "https://forgejo.coilysiren.me"

// forgejoListLimit caps each list/search page ward reads through the REST API,
// matching the survey/scan seams that never needed deep pagination.
const forgejoListLimit = "50"

// issueComment is one row of an issue's comment thread - just the fields the
// reservation check needs: body (for the marker), author, and post time.
type issueComment struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// repoBrief is one row of an owner's repo list - the fields the task-route survey
// and the substrate catalog read: canonical full_name, description, topics.
type repoBrief struct {
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
	Archived    bool     `json:"archived"`
	Empty       bool     `json:"empty"`
}

type forgejoRepoCapabilities struct {
	HasPullRequests bool `json:"has_pull_requests"`
}

// forgejoClient drives Forgejo directly. r resolves host credentials when a
// write needs them, and mode signs the core agent bodies it writes (ward#155).
type forgejoClient struct {
	r       *Runner
	mode    containerMode
	baseURL string
	token   string
}

// hostForgejoClient builds ward's core Forgejo adapter. It intentionally does
// not consult WARD_CONFIG_REF or shell through generated ops leaves (ward#929).
func (r *Runner) hostForgejoClient(ctx context.Context) *forgejoClient {
	cl := &forgejoClient{r: r, mode: currentAgentMode(), baseURL: forgejoBaseURL}
	if tok, err := r.resolveForgejoToken(ctx, broker.Target{}, forgeForgejo); err == nil && strings.TrimSpace(tok) != "" {
		cl.token = tok
	}
	return cl
}

// withMode pins the signing identity for callers that know the mode rather than
// inheriting it from the container env. Returns the client.
func (c *forgejoClient) withMode(m containerMode) *forgejoClient {
	c.mode = m
	return c
}

// withToken pins an already-resolved Forgejo token for direct API calls.
func (c *forgejoClient) withToken(token string) *forgejoClient {
	c.token = token
	return c
}

// apiToken resolves the Forgejo API token for direct HTTP calls, preferring an
// already-pinned token and falling back to the regular Forgejo token resolver.
func (c *forgejoClient) apiToken(ctx context.Context) (string, error) {
	if tok := strings.TrimSpace(c.token); tok != "" {
		return tok, nil
	}
	if c.r == nil {
		return "", fmt.Errorf("no Forgejo token available")
	}
	tok, err := c.r.resolveForgejoToken(ctx, broker.Target{}, forgeForgejo)
	if err != nil {
		return "", err
	}
	c.token = tok
	return tok, nil
}

// optionalAPIToken returns a token already in this process. Read paths may use
// it when present, but they do not fail closed on missing host SSM credentials.
func (c *forgejoClient) optionalAPIToken() string {
	if tok := strings.TrimSpace(c.token); tok != "" {
		return tok
	}
	return ""
}

func (c *forgejoClient) apiBaseURL() string {
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func (c *forgejoClient) apiURL(segments ...string) string {
	escaped := make([]string, 0, len(segments)+2)
	escaped = append(escaped, "api", "v1")
	for _, seg := range segments {
		escaped = append(escaped, url.PathEscape(seg))
	}
	return c.apiBaseURL() + "/" + strings.Join(escaped, "/")
}

func (c *forgejoClient) doJSON(ctx context.Context, method string, segments []string, query url.Values, body any, requireToken bool, out any) ([]byte, error) {
	rdr, err := jsonBodyReader(body)
	if err != nil {
		return nil, err
	}
	endpoint := c.apiURL(segments...)
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rdr)
	if err != nil {
		return nil, err
	}
	if err := c.addAPIHeaders(ctx, req, body != nil, requireToken); err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return readAPIResponse(resp, method, segments, out)
}

// getRaw GETs one Forgejo endpoint and returns the body without JSON decoding.
// Callers use this for text/plain and other opaque bodies.
func (c *forgejoClient) getRaw(ctx context.Context, segments []string, accept string) ([]byte, error) {
	token, err := c.apiToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(segments...), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("forgejo: read %s from %s: %w", apiPath(segments), resp.Status, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("forgejo get %s returned %s after %d byte(s): %s", apiPath(segments), resp.Status, len(data), responseSnippet(data))
	}
	return data, nil
}

func jsonBodyReader(body any) (io.Reader, error) {
	if body == nil {
		return http.NoBody, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(payload), nil
}

func (c *forgejoClient) addAPIHeaders(ctx context.Context, req *http.Request, hasBody bool, requireToken bool) error {
	req.Header.Set("Accept", "application/json")
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if !requireToken {
		if tok := c.optionalAPIToken(); tok != "" {
			req.Header.Set("Authorization", "token "+tok)
		}
		return nil
	}
	tok, err := c.apiToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+tok)
	return nil
}

func readAPIResponse(resp *http.Response, method string, segments []string, out any) ([]byte, error) {
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("forgejo: read %s %s from %s: %w", method, apiPath(segments), resp.Status, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("forgejo %s %s returned %s after %d byte(s): %s", method, apiPath(segments), resp.Status, len(data), responseSnippet(data))
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return data, fmt.Errorf("forgejo: parse %s %s from %s after %d byte(s): %s: %w", method, apiPath(segments), resp.Status, len(data), responseSnippet(data), err)
		}
	}
	return data, nil
}

func apiPath(segments []string) string {
	escaped := make([]string, 0, len(segments)+2)
	escaped = append(escaped, "api", "v1")
	for _, seg := range segments {
		escaped = append(escaped, url.PathEscape(seg))
	}
	return "/" + strings.Join(escaped, "/")
}

// fetchIssueByForge GETs an issue from the selected forge and decodes it into
// dispatch.Issue, the advisor-path resolve seam sharing the dispatch retry (ward#497).
func (r *Runner) fetchIssueByForge(ctx context.Context, label string, f forge, mode containerMode, owner, repo string, number int) (*dispatch.Issue, error) {
	cl, err := r.hostTrackerClient(ctx, trackerFromForge(f), mode)
	if err != nil {
		return nil, err
	}
	ref := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	return resolveIssueWithRetry(label, ref, resolveIssueSleep, func() (*dispatch.Issue, error) {
		return cl.getIssue(ctx, owner, repo, number)
	})
}

// getIssue reads one issue and decodes the rendered JSON. Labels arrive as
// objects, so they decode into a shadow field and flatten to the name list.
func (c *forgejoClient) getIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	var raw struct {
		dispatch.Issue
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if _, err := c.doJSON(ctx, http.MethodGet, []string{"repos", owner, repo, "issues", strconv.Itoa(number)}, nil, nil, false, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	issue := raw.Issue
	for _, l := range raw.Labels {
		issue.Labels = append(issue.Labels, l.Name)
	}
	return &issue, nil
}

// getPullRequestMergeability reads one pull request's mergeability bit directly.
// Forgejo REST data lets the merge gate see whether the base branch still accepts it.
func (c *forgejoClient) getPullRequestMergeability(ctx context.Context, owner, repo string, number int) (*forgejoPullRequestRaw, error) {
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls/%d", baseURL, url.PathEscape(owner), url.PathEscape(repo), number)
	client := &http.Client{Timeout: 30 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		raw, retryable, err := c.getPullRequestMergeabilityOnce(ctx, client, endpoint, owner, repo, number)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !retryable || attempt == 3 {
			return nil, err
		}
		time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
	}
	return nil, lastErr
}

func (c *forgejoClient) getPullRequestMergeabilityOnce(ctx context.Context, client *http.Client, endpoint, owner, repo string, number int) (*forgejoPullRequestRaw, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, false, fmt.Errorf("forgejo: read pull request %s/%s#%d from %s: %w", owner, repo, number, resp.Status, readErr)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, fmt.Errorf("forgejo: pull request %s/%s#%d not found after %d byte(s): %s", owner, repo, number, len(data), responseSnippet(data))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("forgejo pull request GET returned %s after %d byte(s): %s", resp.Status, len(data), responseSnippet(data))
	}
	var raw forgejoPullRequestRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, true, fmt.Errorf("forgejo: parse pull request %s/%s#%d from %s after %d byte(s): %s: %w", owner, repo, number, resp.Status, len(data), responseSnippet(data), err)
	}
	return &raw, false, nil
}

// listIssueComments fetches an issue's comment thread, oldest first.
func (c *forgejoClient) listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	var comments []issueComment
	if _, err := c.doJSON(ctx, http.MethodGet, []string{"repos", owner, repo, "issues", strconv.Itoa(number), "comments"}, nil, nil, false, &comments); err != nil {
		return nil, fmt.Errorf("forgejo: list comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	return comments, nil
}

// createIssue opens a new issue and returns its number. Title+body ride a
// direct JSON request; the body is signed first (ward#155).
func (c *forgejoClient) createIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
	var created struct {
		Number int `json:"number"`
	}
	body = c.mode.signBody(body)
	if _, err := c.doJSON(ctx, http.MethodPost, []string{"repos", owner, repo, "issues"}, nil, map[string]string{"title": title, "body": body}, true, &created); err != nil {
		return 0, fmt.Errorf("forgejo: create issue in %s/%s: %w", owner, repo, err)
	}
	return created.Number, nil
}

// commentIssue appends a signed comment to an existing issue.
func (c *forgejoClient) commentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	if _, err := c.doJSON(ctx, http.MethodPost, []string{"repos", owner, repo, "issues", strconv.Itoa(number), "comments"}, nil, map[string]string{"body": c.mode.signBody(body)}, true, nil); err != nil {
		return fmt.Errorf("forgejo: comment issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// closeIssue flips an existing issue to the closed state (the fixed-body close
// toggle), used by the task route flow to retire an intake record once linked.
func (c *forgejoClient) closeIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.doJSON(ctx, http.MethodPatch, []string{"repos", owner, repo, "issues", strconv.Itoa(number)}, nil, map[string]string{"state": "closed"}, true, nil); err != nil {
		return fmt.Errorf("forgejo: close issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// reopenIssue flips a closed issue back open (the fixed-body reopen toggle); the
// reaper uses it to undo a `closes #N` when a granted repo failed to land (ward#291).
func (c *forgejoClient) reopenIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.doJSON(ctx, http.MethodPatch, []string{"repos", owner, repo, "issues", strconv.Itoa(number)}, nil, map[string]string{"state": "open"}, true, nil); err != nil {
		return fmt.Errorf("forgejo: reopen issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

func (c *forgejoClient) repoPullRequestsEnabled(ctx context.Context, owner, repo string) (bool, error) {
	var caps forgejoRepoCapabilities
	if _, err := c.doJSON(ctx, http.MethodGet, []string{"repos", owner, repo}, nil, nil, false, &caps); err != nil {
		return false, fmt.Errorf("forgejo: get repo %s/%s: %w", owner, repo, err)
	}
	return caps.HasPullRequests, nil
}

func (c *forgejoClient) createPullRequest(ctx context.Context, owner, repo, head, base, title, body string) (string, error) {
	token, err := c.apiToken(ctx)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]string{
		"base":  base,
		"head":  head,
		"title": title,
		"body":  c.mode.signBody(body),
	})
	if err != nil {
		return "", err
	}
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls", baseURL, url.PathEscape(owner), url.PathEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("forgejo create PR returned %s: %s", resp.Status, firstLine(string(data)))
	}
	var created struct {
		HTMLURL string `json:"html_url"`
		URL     string `json:"url"`
		Number  int    `json:"number"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return "", fmt.Errorf("forgejo: parse created pull request: %w", err)
	}
	if created.HTMLURL != "" {
		return created.HTMLURL, nil
	}
	if created.URL != "" {
		return created.URL, nil
	}
	if created.Number != 0 {
		return fmt.Sprintf("%s/%s/%s/pulls/%d", baseURL, owner, repo, created.Number), nil
	}
	return "", fmt.Errorf("forgejo create PR response omitted html_url")
}

// mergePullRequest merges an open PR through Forgejo's merge endpoint.
// The director uses it for the narrow ward-owned merge lane.
func (c *forgejoClient) mergePullRequest(ctx context.Context, owner, repo string, index int) error {
	if os.Getenv("WARD_READONLY") == "1" {
		return fmt.Errorf("forgejo: merge PR %s/%s#%d refused from read-only surface; use ward agent director merge", owner, repo, index)
	}
	return c.mergePullRequestWithHead(ctx, owner, repo, index, "")
}

// mergePullRequestWithHead merges an open PR through Forgejo's merge endpoint and
// pins the head commit the director just checked, so a stale PR head cannot land.
func (c *forgejoClient) mergePullRequestWithHead(ctx context.Context, owner, repo string, index int, headSHA string) error {
	token, err := c.apiToken(ctx)
	if err != nil {
		return err
	}
	payloadBody := map[string]string{"do": "merge"}
	if headSHA = strings.TrimSpace(headSHA); headSHA != "" {
		payloadBody["head_commit_id"] = headSHA
	}
	payload, err := json.Marshal(payloadBody)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls/%d/merge", baseURL, url.PathEscape(owner), url.PathEscape(repo), index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("forgejo merge PR returned %s: %s", resp.Status, firstLine(string(data)))
	}
	return nil
}

type forgejoBranch struct {
	Name                string   `json:"name"`
	Protected           bool     `json:"protected"`
	EnableStatusCheck   bool     `json:"enable_status_check"`
	StatusCheckContexts []string `json:"status_check_contexts"`
}

type forgejoCommitCombinedStatus struct {
	State    string                `json:"state"`
	SHA      string                `json:"sha"`
	Total    int                   `json:"total_count"`
	Statuses []forgejoCommitStatus `json:"statuses"`
}

type forgejoCommitStatus struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Status      string `json:"status"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
}

func (st forgejoCommitStatus) effectiveState() string {
	state := strings.ToLower(strings.TrimSpace(st.State))
	if state != "" {
		return state
	}
	return strings.ToLower(strings.TrimSpace(st.Status))
}

func (c *forgejoClient) getBranch(ctx context.Context, owner, repo, name string) (*forgejoBranch, error) {
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	token, err := c.apiToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches/%s", baseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("forgejo: read branch %s/%s@%s from %s: %w", owner, repo, name, resp.Status, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("forgejo get branch returned %s after %d byte(s): %s", resp.Status, len(data), firstLine(string(data)))
	}
	var branch forgejoBranch
	if err := json.Unmarshal(data, &branch); err != nil {
		return nil, fmt.Errorf("forgejo: parse branch %s/%s@%s from %s after %d byte(s): %s: %w", owner, repo, name, resp.Status, len(data), responseSnippet(data), err)
	}
	return &branch, nil
}

func (c *forgejoClient) getCommitCombinedStatus(ctx context.Context, owner, repo, sha string) (*forgejoCommitCombinedStatus, error) {
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	token, err := c.apiToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/commits/%s/status", baseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("forgejo: read commit status %s/%s@%s from %s: %w", owner, repo, sha, resp.Status, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("forgejo commit status returned %s after %d byte(s): %s", resp.Status, len(data), firstLine(string(data)))
	}
	var status forgejoCommitCombinedStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("forgejo: parse commit status %s/%s@%s from %s after %d byte(s): %s: %w", owner, repo, sha, resp.Status, len(data), responseSnippet(data), err)
	}
	return &status, nil
}

type forgejoPullRequest struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	Draft     bool   `json:"draft"`
	Mergeable bool   `json:"mergeable"`
	HTMLURL   string `json:"html_url"`
	Head      struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (pr forgejoPullRequest) ref(owner, repo string) string {
	if pr.Number <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%s#%d", owner, repo, pr.Number)
}

func (pr forgejoPullRequest) headSHA() string { return strings.TrimSpace(pr.Head.SHA) }

func (c *forgejoClient) getPullRequest(ctx context.Context, owner, repo string, index int) (*forgejoPullRequest, error) {
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	token, err := c.apiToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls/%d", baseURL, url.PathEscape(owner), url.PathEscape(repo), index)
	client := &http.Client{Timeout: 30 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		pr, retryable, err := c.getPullRequestOnce(ctx, client, endpoint, owner, repo, index, token)
		if err == nil {
			return pr, nil
		}
		lastErr = err
		if !retryable || attempt == 3 {
			return nil, err
		}
		time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
	}
	return nil, lastErr
}

func (c *forgejoClient) getPullRequestOnce(ctx context.Context, client *http.Client, endpoint, owner, repo string, index int, token string) (*forgejoPullRequest, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, false, fmt.Errorf("forgejo: read pull request %s/%s#%d from %s: %w", owner, repo, index, resp.Status, readErr)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, fmt.Errorf("forgejo: pull request %s/%s#%d not found after %d byte(s): %s", owner, repo, index, len(data), responseSnippet(data))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("forgejo get PR returned %s after %d byte(s): %s", resp.Status, len(data), firstLine(string(data)))
	}
	var pr forgejoPullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, true, fmt.Errorf("forgejo: parse pull request %s/%s#%d from %s after %d byte(s): %s: %w", owner, repo, index, resp.Status, len(data), responseSnippet(data), err)
	}
	return &pr, false, nil
}

// lockIssue is unsupported: Forgejo's API (gitea-1.22 compat) exposes no issue-lock
// leaf, so the reservation road-block stays the marker comment (ward#494, docs).
func (c *forgejoClient) lockIssue(_ context.Context, _, _ string, _ int) error {
	return errForgeLockUnsupported
}

// unlockIssue mirrors lockIssue: no Forgejo API leaf, so the retract is a no-op.
func (c *forgejoClient) unlockIssue(_ context.Context, _, _ string, _ int) error {
	return errForgeLockUnsupported
}

// listOpenIssues reads the shared Forgejo feed for open issues.
// pull_request:null rows stay issues; PR rows are skipped.
func (c *forgejoClient) listOpenIssues(ctx context.Context, owner, repo string, limit int) ([]backlogIssue, error) {
	raw, err := c.listOpenIssueFeed(ctx, owner, repo, limit)
	if err != nil {
		return nil, err
	}
	issues := make([]backlogIssue, 0, len(raw))
	for _, ri := range raw {
		if ri.isPullRequest() {
			continue
		}
		bi := backlogIssue{Number: ri.Number, Kind: backlogKindIssue, Title: ri.Title, Body: ri.Body, URL: ri.HTMLURL}
		for _, l := range ri.Labels {
			if l.Name != "" {
				bi.Labels = append(bi.Labels, l.Name)
			}
		}
		issues = append(issues, bi)
	}
	return issues, nil
}

// listOpenPullRequests lists a repo's open pull requests with the same lean
// shape as issues, filtered by the shared Forgejo issue classifier.
func (c *forgejoClient) listOpenPullRequests(ctx context.Context, owner, repo string, limit int) ([]directorPullRequest, error) {
	raw, err := c.listOpenIssueFeed(ctx, owner, repo, limit)
	if err != nil {
		return nil, err
	}
	prs := make([]directorPullRequest, 0, len(raw))
	for _, ri := range raw {
		if !ri.isPullRequest() {
			continue
		}
		pr := directorPullRequest{
			Issue: dispatch.Issue{
				Number: ri.Number,
				Title:  ri.Title,
				Body:   ri.Body,
				State:  ri.State,
				URL:    ri.HTMLURL,
			},
		}
		for _, l := range ri.Labels {
			if l.Name != "" {
				pr.Labels = append(pr.Labels, l.Name)
			}
		}
		detail, err := c.getPullRequestMergeability(ctx, owner, repo, ri.Number)
		if err != nil {
			pr.MergeableError = firstLine(err.Error())
			prs = append(prs, pr)
			continue
		}
		pr.Mergeable = detail.Mergeable
		pr.MergeableKnown = true
		prs = append(prs, pr)
	}
	return prs, nil
}

func (c *forgejoClient) listOpenIssueFeed(ctx context.Context, owner, repo string, limit int) ([]forgejoIssueRaw, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{"state": {"open"}, "limit": {strconv.Itoa(limit)}}
	var raw []forgejoIssueRaw
	if _, err := c.doJSON(ctx, http.MethodGet, []string{"repos", owner, repo, "issues"}, q, nil, false, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: list open issues in %s/%s: %w", owner, repo, err)
	}
	return raw, nil
}

// addIssueLabels adds the labels (by name) to an open issue - the write side of startup
// triage (ward#397); an undefined label errors, up to the best-effort caller.
func (c *forgejoClient) addIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	if _, err := c.doJSON(ctx, http.MethodPost, []string{"repos", owner, repo, "issues", strconv.Itoa(number), "labels"}, nil, map[string][]string{"labels": labels}, true, nil); err != nil {
		return fmt.Errorf("forgejo: add labels %v to %s/%s#%d: %w", labels, owner, repo, number, err)
	}
	return nil
}

// listOwnerRepos lists an owner's repos, trying the org leaf then the user leaf
// (the survey's primary owners are both - coilyco-* orgs and the coilysiren user).
func (c *forgejoClient) listOwnerRepos(ctx context.Context, owner string) ([]repoBrief, error) {
	var lastErr error
	for _, shape := range []struct {
		label    string
		segments []string
	}{
		{"org", []string{"orgs", owner, "repos"}},
		{"user", []string{"users", owner, "repos"}},
	} {
		var repos []repoBrief
		q := url.Values{"limit": {forgejoListLimit}}
		if _, err := c.doJSON(ctx, http.MethodGet, shape.segments, q, nil, false, &repos); err != nil {
			// A 404 means the owner is not that kind (org vs user); try the next
			// shape before surfacing the failure.
			lastErr = fmt.Errorf("%s repos: %w", shape.label, err)
			continue
		}
		return repos, nil
	}
	return nil, lastErr
}

// leanIssueView is the {issue, comments} projection the `issue view` override
// prints, trimmed to usernames so a reader isn't handed a full profile per comment.
type leanIssueView struct {
	Issue    leanIssue     `json:"issue"`
	Comments []leanComment `json:"comments"`
}

// leanIssue is the issue half of leanIssueView (ward#225).
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

// leanComment is one thread row of leanIssueView - author login, time, body.
type leanComment struct {
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"body"`
}

// forgejoIssueRaw is the slice of Forgejo's issue JSON leanIssue projects from.
type forgejoIssueRaw struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	HTMLURL     string    `json:"html_url"`
	Comments    int       `json:"comments"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PullRequest *struct{} `json:"pull_request"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

func (raw forgejoIssueRaw) isPullRequest() bool {
	return raw.PullRequest != nil
}

// forgejoPullRequestRaw is the focused PR detail shape used for mergeability.
type forgejoPullRequestRaw struct {
	Mergeable bool `json:"mergeable"`
}

// directorPullRequest is the open PR list projection used by the director merge
// lane. It keeps the issue surface plus the focused mergeability bit.
type directorPullRequest struct {
	dispatch.Issue
	Mergeable      bool
	MergeableKnown bool
	MergeableError string
}

// lean projects the raw issue down to the reader-facing leanIssue.
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
// (ward#225).
func (c *forgejoClient) viewIssue(ctx context.Context, owner, repo string, number int) (*leanIssueView, error) {
	var raw forgejoIssueRaw
	if _, err := c.doJSON(ctx, http.MethodGet, []string{"repos", owner, repo, "issues", strconv.Itoa(number)}, nil, nil, false, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: view issue %s/%s#%d: %w", owner, repo, number, err)
	}
	comments, err := c.listIssueComments(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	return leanView(raw, comments), nil
}

// leanView projects a raw issue plus its comment thread to the reader-facing
// {issue, comments} shape - the pure half of viewIssue (ward#225).
func leanView(raw forgejoIssueRaw, comments []issueComment) *leanIssueView {
	view := &leanIssueView{Issue: raw.lean(), Comments: make([]leanComment, 0, len(comments))}
	for _, cm := range comments {
		view.Comments = append(view.Comments, leanComment{
			User:      cm.User.Login,
			CreatedAt: cm.CreatedAt,
			Body:      cm.Body,
		})
	}
	return view
}

func responseSnippet(data []byte) string {
	if len(data) == 0 {
		return "<empty>"
	}
	line := firstLine(string(data))
	if line == "" {
		return "<empty>"
	}
	const maxSnippet = 160
	if len(line) > maxSnippet {
		line = line[:maxSnippet] + "..."
	}
	return strconv.Quote(line)
}
