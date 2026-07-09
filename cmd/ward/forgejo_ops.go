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
	"os/exec"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
)

// forgejo_ops.go is ward's Forgejo client, routed through the in-binary `ward ops
// forgejo` guardfile runtime (ward#92). See docs/ops-forgejo-in-ward.md.

// forgejoBaseURL is the Forgejo origin, used to render issue URLs and parse refs.
// Safe to hardcode; the bearer token resolves in the subprocess, not here.
const forgejoBaseURL = "https://forgejo.coilysiren.me"

// forgejoListLimit caps each list/search page ward reads through the ops mount,
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

// forgejoClient drives Forgejo through `ward ops forgejo`. exe is the resolved
// ward binary, r runs it audited, and mode signs the bodies it writes (ward#155).
type forgejoClient struct {
	r       *Runner
	exe     string
	mode    containerMode
	baseURL string
	token   string
}

// hostForgejoClient builds a client over the in-binary ops mount; auth resolves in
// the subprocess (see forgejoTokenResolver). ctx is unused, kept for call sites.
func (r *Runner) hostForgejoClient(_ context.Context) (*forgejoClient, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("forgejo: resolve ward binary: %w", err)
	}
	// Shell back to canonical ward, not the invoked `warded` shim, so the `ops`
	// call skips the warded->`ward agent` rewrite that rejects --output (ward#304).
	exe = canonicalWardExe(exe)
	return &forgejoClient{r: r, exe: exe, mode: currentAgentMode(), baseURL: forgejoBaseURL}, nil
}

// withMode pins the signing identity for callers that know the mode rather than
// inheriting it from the container env. Returns the client.
func (c *forgejoClient) withMode(m containerMode) *forgejoClient {
	c.mode = m
	return c
}

// withToken pins the already-resolved container Forgejo token for direct API
// leaves that are not present in the generated ops surface.
func (c *forgejoClient) withToken(token string) *forgejoClient {
	c.token = token
	return c
}

// run shells the ward binary back to its `ops forgejo` mount, returning stdout. On a
// non-zero exit it folds the subprocess stderr into the error (ward#596, docs/broker.md).
func (c *forgejoClient) run(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"ops", "forgejo"}, args...)
	var stdout, stderr bytes.Buffer
	// #nosec G204 -- c.exe is the resolved canonical ward binary, argv is fixed verbs.
	cmd := exec.CommandContext(ctx, c.exe, full...)
	cmd.Stdout = &stdout
	// Tee stderr: keep it streaming live (interactive/host runs keep their output)
	// while capturing a copy so a failure can name the envelope, not just the code.
	if live := c.r.Runner.Stderr; live != nil {
		cmd.Stderr = io.MultiWriter(live, &stderr)
	} else {
		cmd.Stderr = &stderr
	}
	cmd.Stdin = c.r.Runner.Stdin
	if c.r.Runner.Env != nil {
		cmd.Env = append(os.Environ(), c.r.Runner.Env...)
	}
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), foldOpsStderr(err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// foldOpsStderr appends a subprocess's captured stderr to its exit error, so a caller
// reads the cli-guard envelope, not a bare `exit status N`. Empty stderr: unchanged.
func foldOpsStderr(err error, stderr []byte) error {
	detail := condenseOpsStderr(stderr)
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

// condenseOpsStderr trims captured stderr to one line: blank lines drop, the rest join
// with "; ", capped so a long envelope can't flood the surface's error (ward#596).
func condenseOpsStderr(stderr []byte) string {
	const maxLen = 800
	var kept []string
	for _, ln := range strings.Split(string(stderr), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			kept = append(kept, ln)
		}
	}
	joined := strings.Join(kept, "; ")
	if len(joined) > maxLen {
		joined = joined[:maxLen] + "..."
	}
	return joined
}

// fetchForgejoIssue GETs a Forgejo issue and decodes it into dispatch.Issue, the
// advisor-path resolve seam sharing the dispatch retry (ward#497).
func (r *Runner) fetchForgejoIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return nil, err
	}
	ref := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	return resolveIssueWithRetry("ward agent advisor", ref, resolveIssueSleep, func() (*dispatch.Issue, error) {
		return cl.getIssue(ctx, owner, repo, number)
	})
}

// getIssue reads one issue and decodes the rendered JSON. Labels arrive as
// objects, so they decode into a shadow field and flatten to the name list.
func (c *forgejoClient) getIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	out, err := c.run(ctx, "issue", "get", owner, repo, strconv.Itoa(number), "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("forgejo: get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	var raw struct {
		dispatch.Issue
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: parse issue %s/%s#%d: %w", owner, repo, number, err)
	}
	issue := raw.Issue
	for _, l := range raw.Labels {
		issue.Labels = append(issue.Labels, l.Name)
	}
	return &issue, nil
}

// listIssueComments fetches an issue's comment thread, oldest first.
func (c *forgejoClient) listIssueComments(ctx context.Context, owner, repo string, number int) ([]issueComment, error) {
	out, err := c.run(ctx, "issue-comment", "list", owner, repo, strconv.Itoa(number), "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("forgejo: list comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	var comments []issueComment
	if err := json.Unmarshal(out, &comments); err != nil {
		return nil, fmt.Errorf("forgejo: parse comments on %s/%s#%d: %w", owner, repo, number, err)
	}
	return comments, nil
}

// createIssue opens a new issue and returns its number. Title+body ride a
// --body-file (clears the argv metachar gate); the body is signed first (ward#155).
func (c *forgejoClient) createIssue(ctx context.Context, owner, repo, title, body string) (int, error) {
	path, cleanup, err := writeForgejoBody(map[string]string{"title": title, "body": c.mode.signBody(body)})
	if err != nil {
		return 0, err
	}
	defer cleanup()
	out, err := c.run(ctx, "issue", "create", owner, repo, "--body-file", path, "--output", "json")
	if err != nil {
		return 0, fmt.Errorf("forgejo: create issue in %s/%s: %w", owner, repo, err)
	}
	var created struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		return 0, fmt.Errorf("forgejo: parse created issue: %w", err)
	}
	return created.Number, nil
}

// commentIssue appends a comment to an existing issue. The body rides a
// --body-file (same argv-gate reason as createIssue) and is signed first.
func (c *forgejoClient) commentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	path, cleanup, err := writeForgejoBody(map[string]string{"body": c.mode.signBody(body)})
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := c.run(ctx, "issue", "comment", owner, repo, strconv.Itoa(number), "--body-file", path); err != nil {
		return fmt.Errorf("forgejo: comment issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// closeIssue flips an existing issue to the closed state (the fixed-body close
// toggle), used by the task route flow to retire an intake record once linked.
func (c *forgejoClient) closeIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "issue", "close", owner, repo, strconv.Itoa(number)); err != nil {
		return fmt.Errorf("forgejo: close issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

// reopenIssue flips a closed issue back open (the fixed-body reopen toggle); the
// reaper uses it to undo a `closes #N` when a granted repo failed to land (ward#291).
func (c *forgejoClient) reopenIssue(ctx context.Context, owner, repo string, number int) error {
	if _, err := c.run(ctx, "issue", "reopen", owner, repo, strconv.Itoa(number)); err != nil {
		return fmt.Errorf("forgejo: reopen issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

func (c *forgejoClient) repoPullRequestsEnabled(ctx context.Context, owner, repo string) (bool, error) {
	out, err := c.run(ctx, "repo", "get", owner, repo, "--output", "json")
	if err != nil {
		return false, fmt.Errorf("forgejo: get repo %s/%s: %w", owner, repo, err)
	}
	var caps forgejoRepoCapabilities
	if err := json.Unmarshal(out, &caps); err != nil {
		return false, fmt.Errorf("forgejo: parse repo %s/%s: %w", owner, repo, err)
	}
	return caps.HasPullRequests, nil
}

func (c *forgejoClient) createPullRequest(ctx context.Context, owner, repo, head, base, title, body string) (string, error) {
	if strings.TrimSpace(c.token) == "" {
		return "", fmt.Errorf("no Forgejo token available")
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
	req.Header.Set("Authorization", "token "+c.token)
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
	if strings.TrimSpace(c.token) == "" {
		return fmt.Errorf("no Forgejo token available")
	}
	payload, err := json.Marshal(map[string]string{
		"do": "merge",
	})
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
	req.Header.Set("Authorization", "token "+c.token)
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

// lockIssue is unsupported: Forgejo's API (gitea-1.22 compat) exposes no issue-lock
// leaf, so the reservation road-block stays the marker comment (ward#494, docs).
func (c *forgejoClient) lockIssue(_ context.Context, _, _ string, _ int) error {
	return errForgeLockUnsupported
}

// unlockIssue mirrors lockIssue: no Forgejo API leaf, so the retract is a no-op.
func (c *forgejoClient) unlockIssue(_ context.Context, _, _ string, _ int) error {
	return errForgeLockUnsupported
}

// listOpenIssues lists a repo's open issues (not pulls) with their labels, the
// backlog loop's ranking input (ward#346). Mirrors backlog-loop.py's fetch.
func (c *forgejoClient) listOpenIssues(ctx context.Context, owner, repo string, limit int) ([]backlogIssue, error) {
	if limit <= 0 {
		limit = 50
	}
	out, err := c.run(ctx, "issue", "list", owner, repo, "--state", "open", "--type", "issues", "--limit", strconv.Itoa(limit), "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("forgejo: list open issues in %s/%s: %w", owner, repo, err)
	}
	var raw []forgejoIssueRaw
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: parse issue list for %s/%s: %w", owner, repo, err)
	}
	issues := make([]backlogIssue, 0, len(raw))
	for _, ri := range raw {
		bi := backlogIssue{Number: ri.Number, Title: ri.Title, Body: ri.Body, URL: ri.HTMLURL}
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
// shape as issues, but filtered to type=pulls for the director merge lane.
func (c *forgejoClient) listOpenPullRequests(ctx context.Context, owner, repo string, limit int) ([]dispatch.Issue, error) {
	if limit <= 0 {
		limit = 50
	}
	out, err := c.run(ctx, "issue", "list", owner, repo, "--state", "open", "--type", "pulls", "--limit", strconv.Itoa(limit), "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("forgejo: list open pull requests in %s/%s: %w", owner, repo, err)
	}
	var raw []forgejoIssueRaw
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: parse pull request list for %s/%s: %w", owner, repo, err)
	}
	prs := make([]dispatch.Issue, 0, len(raw))
	for _, ri := range raw {
		pr := dispatch.Issue{
			Number: ri.Number,
			Title:  ri.Title,
			Body:   ri.Body,
			State:  ri.State,
			URL:    ri.HTMLURL,
		}
		for _, l := range ri.Labels {
			if l.Name != "" {
				pr.Labels = append(pr.Labels, l.Name)
			}
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// addIssueLabels adds the labels (by name) to an open issue - the write side of startup
// triage (ward#397); an undefined label errors, up to the best-effort caller.
func (c *forgejoClient) addIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	args := []string{"issue-label", "add", owner, repo, strconv.Itoa(number)}
	for _, l := range labels {
		args = append(args, "--labels", l)
	}
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("forgejo: add labels %v to %s/%s#%d: %w", labels, owner, repo, number, err)
	}
	return nil
}

// removeIssueLabels removes the labels (by name) from an open issue - the take-off side
// of the consult interview's mode flip (ward#493), mirroring addIssueLabels.
func (c *forgejoClient) removeIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	args := []string{"issue-label", "remove", owner, repo, strconv.Itoa(number)}
	for _, l := range labels {
		args = append(args, "--labels", l)
	}
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("forgejo: remove labels %v from %s/%s#%d: %w", labels, owner, repo, number, err)
	}
	return nil
}

// listOwnerRepos lists an owner's repos, trying the org leaf then the user leaf
// (the survey's primary owners are both - coilyco-* orgs and the coilysiren user).
func (c *forgejoClient) listOwnerRepos(ctx context.Context, owner string) ([]repoBrief, error) {
	var lastErr error
	for _, leaf := range []string{"org-repo", "user-repo"} {
		out, err := c.run(ctx, leaf, "list", owner, "--limit", forgejoListLimit, "--output", "json")
		if err != nil {
			// A 404 means the owner is not that kind (org vs user); try the next
			// shape before surfacing the failure.
			lastErr = err
			continue
		}
		var repos []repoBrief
		if err := json.Unmarshal(out, &repos); err != nil {
			return nil, fmt.Errorf("forgejo: parse repos for %s: %w", owner, err)
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
// (ward#225), reading both through the ops mount.
func (c *forgejoClient) viewIssue(ctx context.Context, owner, repo string, number int) (*leanIssueView, error) {
	out, err := c.run(ctx, "issue", "get", owner, repo, strconv.Itoa(number), "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("forgejo: view issue %s/%s#%d: %w", owner, repo, number, err)
	}
	var raw forgejoIssueRaw
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("forgejo: parse issue %s/%s#%d: %w", owner, repo, number, err)
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

// writeForgejoBody marshals a request body to a temp JSON file for --body-file,
// returning the path and a cleanup that removes it. Keeps markdown off the argv gate.
func writeForgejoBody(obj map[string]string) (path string, cleanup func(), err error) {
	noop := func() {}
	f, err := os.CreateTemp("", "ward-forgejo-body-*.json")
	if err != nil {
		return "", noop, fmt.Errorf("forgejo: create body file: %w", err)
	}
	remove := func() { _ = os.Remove(f.Name()) }
	if err := json.NewEncoder(f).Encode(obj); err != nil {
		_ = f.Close()
		remove()
		return "", noop, fmt.Errorf("forgejo: write body file: %w", err)
	}
	if err := f.Close(); err != nil {
		remove()
		return "", noop, fmt.Errorf("forgejo: close body file: %w", err)
	}
	return f.Name(), remove, nil
}
