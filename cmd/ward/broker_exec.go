package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/credseed"
)

// broker_exec.go is the privileged half of ward's root credential broker: the
// injected broker.Executor + broker.Authorizer (ward#329). See docs/broker.md.

// wardKdlWriteExecutor is the broker.Executor: it mutates Forgejo through ward's
// typed core adapter, holding the bot token in memory (docs/broker.md).
type wardKdlWriteExecutor struct {
	// token is the root-held forgejo bot credential, seeded into FORGEJO_TOKEN.
	token string
	// baseURL is test-only; production uses forgejoBaseURL.
	baseURL string
}

func (e *wardKdlWriteExecutor) client() *forgejoClient {
	baseURL := strings.TrimRight(e.baseURL, "/")
	if baseURL == "" {
		baseURL = forgejoBaseURL
	}
	return &forgejoClient{token: e.token, baseURL: baseURL}
}

// FileIssue files a new issue under target; the JSON projection yields the
// forge-assigned number + html_url for the broker.Result.
func (e *wardKdlWriteExecutor) FileIssue(ctx context.Context, target broker.Target, title, body string) (broker.Result, error) {
	out, err := e.client().doJSON(ctx, "POST",
		[]string{"repos", target.Owner, target.Repo, "issues"}, nil,
		map[string]string{"title": title, "body": body}, true, nil)
	if err != nil {
		return broker.Result{}, fmt.Errorf("broker: create issue %s/%s: %w", target.Owner, target.Repo, err)
	}
	return parseIssueResult(out), nil
}

// EditIssue edits target's issue; an empty title/body/state leaves that field
// untouched, per the broker.Executor contract.
func (e *wardKdlWriteExecutor) EditIssue(ctx context.Context, target broker.Target, title, body, state string) (broker.Result, error) {
	payload := map[string]string{}
	if title != "" {
		payload["title"] = title
	}
	if body != "" {
		payload["body"] = body
	}
	if state != "" {
		payload["state"] = state
	}
	segments, err := e.editTargetSegments(ctx, target)
	if err != nil {
		return broker.Result{}, err
	}
	out, err := e.client().doJSON(ctx, http.MethodPatch, segments, nil, payload, true, nil)
	if err != nil {
		return broker.Result{}, fmt.Errorf("broker: edit issue %s/%s#%d: %w", target.Owner, target.Repo, target.Number, err)
	}
	return parseIssueResult(out), nil
}

// editTargetSegments resolves whether the target number is an issue or a PR so
// the broker can reach Forgejo's native endpoint for both without changing the wire op.
func (e *wardKdlWriteExecutor) editTargetSegments(ctx context.Context, target broker.Target) ([]string, error) {
	var raw forgejoIssueRaw
	if _, err := e.client().doJSON(ctx, http.MethodGet, []string{"repos", target.Owner, target.Repo, "issues", strconv.Itoa(target.Number)}, nil, nil, true, &raw); err != nil {
		return nil, fmt.Errorf("broker: detect issue type %s/%s#%d: %w", target.Owner, target.Repo, target.Number, err)
	}
	if raw.PullRequest != nil {
		return []string{"repos", target.Owner, target.Repo, "pulls", strconv.Itoa(target.Number)}, nil
	}
	return []string{"repos", target.Owner, target.Repo, "issues", strconv.Itoa(target.Number)}, nil
}

// CommentIssue posts body via Forgejo's issue-comment endpoint.
func (e *wardKdlWriteExecutor) CommentIssue(ctx context.Context, target broker.Target, body string) (broker.Result, error) {
	out, err := e.client().doJSON(ctx, "POST",
		[]string{"repos", target.Owner, target.Repo, "issues", strconv.Itoa(target.Number), "comments"}, nil,
		map[string]string{"body": body}, true, nil)
	if err != nil {
		return broker.Result{}, fmt.Errorf("broker: comment issue %s/%s#%d: %w", target.Owner, target.Repo, target.Number, err)
	}
	res := parseCommentResult(out)
	if res.Number == 0 {
		res.Number = target.Number
	}
	return res, nil
}

// LabelIssue mutates target issue's label membership by mode through Forgejo.
// cli-guard's labelInvariants already gated it fail-closed.
func (e *wardKdlWriteExecutor) LabelIssue(ctx context.Context, target broker.Target, mode string, labels []string) (broker.Result, error) {
	cl := e.client()
	segments := []string{"repos", target.Owner, target.Repo, "issues", strconv.Itoa(target.Number), "labels"}
	var out []byte
	var err error
	switch mode {
	case broker.LabelAdd:
		out, err = cl.doJSON(ctx, "POST", segments, nil, map[string][]string{"labels": labels}, true, nil)
	case broker.LabelSet:
		out, err = cl.doJSON(ctx, "PUT", segments, nil, map[string][]string{"labels": labels}, true, nil)
	case broker.LabelRemove:
		for _, label := range labels {
			out, err = cl.doJSON(ctx, "DELETE", append(segments, label), nil, nil, true, nil)
			if err != nil {
				break
			}
		}
	default:
		return broker.Result{}, fmt.Errorf("broker: unsupported label mode %q", mode)
	}
	if err != nil {
		return broker.Result{}, fmt.Errorf("broker: label issue %s/%s#%d: %w", target.Owner, target.Repo, target.Number, err)
	}
	// The label leaf returns the issue's label array, not an {number} object, so
	// parseIssueResult falls to Detail; reuse the target number for the rendered ref.
	res := parseIssueResult(out)
	if res.Number == 0 {
		res.Number = target.Number
	}
	return res, nil
}

// Dispatch vends the seed for target - the root-held forge token on Result.Detail,
// so a `warded #N` seeds its child env-file broker-side (ward#334; docs/broker.md).
func (e *wardKdlWriteExecutor) Dispatch(_ context.Context, _ broker.Target) (broker.Result, error) {
	if e.token == "" {
		return broker.Result{}, fmt.Errorf("broker: dispatch seed unavailable: no %s held", credseed.EnvForgejoToken)
	}
	return broker.Result{Detail: e.token}, nil
}

// parseCommentResult lifts the html_url from the `issue comment` shadow's
// {comment, issue} JSON (ward#613), falling back to parseIssueResult's flat shape.
func parseCommentResult(out []byte) broker.Result {
	var shadow struct {
		Comment struct {
			HTMLURL string `json:"html_url"`
			URL     string `json:"url"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &shadow); err == nil {
		if url := shadow.Comment.HTMLURL; url != "" {
			return broker.Result{URL: url}
		}
		if url := shadow.Comment.URL; url != "" {
			return broker.Result{URL: url}
		}
	}
	return parseIssueResult(out)
}

// parseIssueResult best-effort projects a forgejo issue/comment JSON object into
// a broker.Result; an unparseable payload reports success with the raw body.
func parseIssueResult(out []byte) broker.Result {
	var payload struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &payload); err != nil {
		return broker.Result{Detail: truncate(strings.TrimSpace(string(out)), 200)}
	}
	url := payload.HTMLURL
	if url == "" {
		url = payload.URL
	}
	return broker.Result{Number: payload.Number, URL: url}
}

// writeTierOps is the op allowlist this daemon serves: file / edit / comment / label
// + dispatch (ward#334, ward#625); delete/admin refuse out-of-tier before the executor.
var writeTierOps = map[broker.Op]bool{
	broker.OpFileIssue:    true,
	broker.OpEditIssue:    true,
	broker.OpCommentIssue: true,
	broker.OpLabelIssue:   true,
	broker.OpDispatch:     true,
}

// writeTierAuthorizer is the broker.Authorizer: the write-op allowlist plus the
// owner scope and live-run comment gate.
func (r *Runner) writeTierAuthorizer() broker.Authorizer {
	policy := broker.Policy{Ops: writeTierOps}
	return broker.AuthorizerFunc(func(ctx context.Context, req broker.Request) error {
		if err := policy.Authorize(ctx, req); err != nil {
			return err
		}
		if brokerOwnerPrefix != "" && !strings.HasPrefix(req.Target.Owner, brokerOwnerPrefix) {
			return fmt.Errorf("broker: owner %q is out of scope (restricted to %s* owners)", req.Target.Owner, brokerOwnerPrefix)
		}
		if req.Op == broker.OpCommentIssue && r != nil && r.Runner != nil {
			if err := r.guardReadOnlyIssueComment(ctx, req.Target); err != nil {
				return err
			}
		}
		return nil
	})
}

// guardReadOnlyIssueComment blocks a read-only surface comment when the issue is
// currently owned by a live engineer container or a fresh reservation claim.
func (r *Runner) guardReadOnlyIssueComment(ctx context.Context, target broker.Target) error {
	ref := agentIssueRef{Owner: target.Owner, Repo: target.Repo, Number: target.Number}
	running, err := r.runningEngineerContainersForIssue(ctx, ref)
	if err != nil {
		return fmt.Errorf("broker: check active engineer runs for %s: %w", ref, err)
	}
	comments, err := r.hostForgejoClient(ctx).listIssueComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return fmt.Errorf("broker: read issue comments for %s: %w", ref, err)
	}
	if reason, ok := readOnlyIssueCommentBlockReason(ref, running, comments, time.Now().UTC()); ok {
		return fmt.Errorf("broker: refusing read-only issue comment on %s: %s", ref, reason)
	}
	return nil
}

// runningEngineerContainersForIssue reads the live engineer list for one issue and
// returns the matching container names. It fails closed on docker errors.
func (r *Runner) runningEngineerContainersForIssue(ctx context.Context, ref agentIssueRef) ([]string, error) {
	out, err := r.dockerCapture(ctx, "ps", "--format", "{{.Names}}",
		"--filter", "label="+containerLabel,
		"--filter", "label="+labelRole+"="+roleEngineer,
		"--filter", "label="+labelRepo+"="+ref.repoSlug(),
		"--filter", fmt.Sprintf("label=%s=%d", labelIssue, ref.Number))
	if err != nil {
		return nil, fmt.Errorf("check live engineer containers for %s: %w", ref, err)
	}
	return parseExitedContainerNames(string(out)), nil
}

// readOnlyIssueCommentBlockReason returns the refusal text when a running engineer
// or fresh reservation already owns the issue.
func readOnlyIssueCommentBlockReason(ref agentIssueRef, running []string, comments []issueComment, now time.Time) (string, bool) {
	if reason := runningIssueCommentBlockReason(ref, running); reason != "" {
		return reason, true
	}
	if claim, ok := winningReservationClaim(reservationClaims(comments, now, agentReservationTTL())); ok {
		return reservationIssueCommentBlockReason(ref, claim), true
	}
	return "", false
}

func runningIssueCommentBlockReason(ref agentIssueRef, running []string) string {
	running = compactStrings(running)
	switch len(running) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("running engineer container %s owns this issue; use `ward agent list`, `ward agent logs %s`, or file a separate issue", running[0], ref.String())
	default:
		return fmt.Sprintf("%d running engineer containers (%s) own this issue; use `ward agent list`, `ward agent logs %s`, or file a separate issue", len(running), strings.Join(running, ", "), ref.String())
	}
}

func reservationIssueCommentBlockReason(ref agentIssueRef, claim reservationClaim) string {
	owner := strings.TrimSpace(claim.identity)
	if owner == "" {
		owner = "another engineer run"
	}
	when := claim.at.UTC().Format(time.RFC3339)
	return fmt.Sprintf("fresh reservation %s at %s owns this issue; use `ward agent list`, `ward agent logs %s`, or file a separate issue", owner, when, ref.String())
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}
