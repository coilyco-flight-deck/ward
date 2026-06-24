package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// repoBrief is one row of an owner's repo list - just the fields the task route
// survey needs to build a catalog the agent picks from (ward#164).
type repoBrief struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	Empty       bool   `json:"empty"`
}

// forgejoClient drives Forgejo through `ward ops forgejo`. exe is the resolved
// ward binary, r runs it audited, and mode signs the bodies it writes (ward#155).
type forgejoClient struct {
	r    *Runner
	exe  string
	mode containerMode
}

// hostForgejoClient builds a client over the in-binary ops mount; auth resolves in
// the subprocess (see forgejoTokenResolver). ctx is unused, kept for call sites.
func (r *Runner) hostForgejoClient(_ context.Context) (*forgejoClient, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("forgejo: resolve ward binary: %w", err)
	}
	return &forgejoClient{r: r, exe: exe, mode: currentAgentMode()}, nil
}

// withMode pins the signing identity for callers that know the mode rather than
// inheriting it from the container env. Returns the client.
func (c *forgejoClient) withMode(m containerMode) *forgejoClient {
	c.mode = m
	return c
}

// run shells the ward binary back to its own `ops forgejo` mount and returns the
// captured stdout - the rendered body for reads, the confirmation for writes.
func (c *forgejoClient) run(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"ops", "forgejo"}, args...)
	return c.r.Runner.Capture(ctx, c.exe, full...)
}

// fetchForgejoIssue GETs a Forgejo issue and decodes it into dispatch.Issue, the
// pre-flight resolve seam for `ward agent`.
func (r *Runner) fetchForgejoIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	cl, err := r.hostForgejoClient(ctx)
	if err != nil {
		return nil, err
	}
	return cl.getIssue(ctx, owner, repo, number)
}

// getIssue reads one issue (GET issue) and decodes the rendered JSON body.
func (c *forgejoClient) getIssue(ctx context.Context, owner, repo string, number int) (*dispatch.Issue, error) {
	out, err := c.run(ctx, "issue", "get", owner, repo, strconv.Itoa(number), "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("forgejo: get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	var issue dispatch.Issue
	if err := json.Unmarshal(out, &issue); err != nil {
		return nil, fmt.Errorf("forgejo: parse issue %s/%s#%d: %w", owner, repo, number, err)
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

// findOpenIssueByTitlePrefix returns the first open issue whose title starts with
// prefix, so the reaper appends instead of filing a duplicate salvage issue.
func (c *forgejoClient) findOpenIssueByTitlePrefix(ctx context.Context, owner, repo, prefix string) (number int, found bool, err error) {
	out, err := c.run(ctx, "issue", "list", owner, repo, "--state", "open", "--type", "issues", "--limit", forgejoListLimit, "--output", "json")
	if err != nil {
		return 0, false, fmt.Errorf("forgejo: list issues in %s/%s: %w", owner, repo, err)
	}
	var issues []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(out, &issues); err != nil {
		return 0, false, fmt.Errorf("forgejo: parse issue list for %s/%s: %w", owner, repo, err)
	}
	for _, i := range issues {
		if strings.HasPrefix(i.Title, prefix) {
			return i.Number, true, nil
		}
	}
	return 0, false, nil
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
