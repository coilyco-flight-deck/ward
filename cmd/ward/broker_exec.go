package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/credseed"
)

// broker_exec.go is the privileged half of ward's root credential broker: the
// injected broker.Executor + broker.Authorizer (ward#329). See docs/broker.md.

// wardKdlWriteBin is the write-tier forgejo binary the executor shells: read +
// create/edit, no delete leaf compiled in (ward#240, an out-of-tier verb absent).
const wardKdlWriteBin = "ward-kdl-write"

// cmdRunner runs name with args and env, returning combined output. Injected so
// the executor's argv shaping is unit-testable without the real binary on PATH.
type cmdRunner func(ctx context.Context, name string, args, env []string) ([]byte, error)

// execCmdRunner is the production cmdRunner: run the binary, capture output.
func execCmdRunner(ctx context.Context, name string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- name is a fixed const, args are broker-validated
	cmd.Env = env
	return cmd.CombinedOutput()
}

// wardKdlWriteExecutor is the broker.Executor: it mutates the forge by shelling
// the write binary, holding the bot token and seeding it into env (docs/broker.md).
type wardKdlWriteExecutor struct {
	// token is the root-held forgejo bot credential, seeded into FORGEJO_TOKEN.
	token string
	// run executes the write binary; defaults to execCmdRunner when nil.
	run cmdRunner
}

// exec shells the write binary with the bot token in env (never argv), wrapping
// a failure with the token-free argv and any captured output for the audit log.
func (e *wardKdlWriteExecutor) exec(ctx context.Context, args []string) ([]byte, error) {
	run := e.run
	if run == nil {
		run = execCmdRunner
	}
	env := append(os.Environ(), credseed.EnvForgejoToken+"="+e.token)
	out, err := run(ctx, wardKdlWriteBin, args, env)
	if err != nil {
		return nil, fmt.Errorf("broker: %s %s: %w: %s",
			wardKdlWriteBin, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// FileIssue files a new issue under target; the JSON projection yields the
// forge-assigned number + html_url for the broker.Result.
func (e *wardKdlWriteExecutor) FileIssue(ctx context.Context, target broker.Target, title, body string) (broker.Result, error) {
	args := []string{"ops", "forgejo", "issue", "create", target.Owner, target.Repo, "--title", title}
	if body != "" {
		args = append(args, "--body", body)
	}
	args = append(args, "--output", "json")
	out, err := e.exec(ctx, args)
	if err != nil {
		return broker.Result{}, err
	}
	return parseIssueResult(out), nil
}

// EditIssue edits target's issue; an empty title/body/state leaves that field
// untouched, per the broker.Executor contract.
func (e *wardKdlWriteExecutor) EditIssue(ctx context.Context, target broker.Target, title, body, state string) (broker.Result, error) {
	args := []string{"ops", "forgejo", "issue", "edit", target.Owner, target.Repo, strconv.Itoa(target.Number)}
	if title != "" {
		args = append(args, "--title", title)
	}
	if body != "" {
		args = append(args, "--body", body)
	}
	if state != "" {
		args = append(args, "--state", state)
	}
	args = append(args, "--output", "json")
	out, err := e.exec(ctx, args)
	if err != nil {
		return broker.Result{}, err
	}
	return parseIssueResult(out), nil
}

// CommentIssue posts body as a comment on target's issue. The comment payload
// carries no issue number, so the Result reuses the request's target number.
func (e *wardKdlWriteExecutor) CommentIssue(ctx context.Context, target broker.Target, body string) (broker.Result, error) {
	args := []string{"ops", "forgejo", "comment", "create", target.Owner, target.Repo, strconv.Itoa(target.Number), "--body", body, "--output", "json"}
	out, err := e.exec(ctx, args)
	if err != nil {
		return broker.Result{}, err
	}
	res := parseIssueResult(out)
	if res.Number == 0 {
		res.Number = target.Number
	}
	return res, nil
}

// Dispatch is unserved in Unit B (routing is Unit C); the authorizer already
// rejects OpDispatch, so this is the defensive backstop the contract requires.
func (e *wardKdlWriteExecutor) Dispatch(_ context.Context, _ broker.Target) (broker.Result, error) {
	return broker.Result{}, fmt.Errorf("broker: dispatch is not served by the %s executor (ward#331 Unit B; routing is Unit C)", wardKdlWriteBin)
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

// writeTierOps is the broker op allowlist this daemon serves: file / edit /
// comment. broker.OpDispatch is absent, so a dispatch request is out-of-tier.
var writeTierOps = map[broker.Op]bool{
	broker.OpFileIssue:    true,
	broker.OpEditIssue:    true,
	broker.OpCommentIssue: true,
}

// writeTierAuthorizer is the broker.Authorizer: the write-op allowlist + Policy's
// invariants, plus an owner scope gate mirroring the write guardfile's restrict.
func writeTierAuthorizer(ownerPrefix string) broker.Authorizer {
	policy := broker.Policy{Ops: writeTierOps}
	return broker.AuthorizerFunc(func(ctx context.Context, req broker.Request) error {
		if err := policy.Authorize(ctx, req); err != nil {
			return err
		}
		if ownerPrefix != "" && !strings.HasPrefix(req.Target.Owner, ownerPrefix) {
			return fmt.Errorf("broker: owner %q is out of scope (restricted to %s* owners)", req.Target.Owner, ownerPrefix)
		}
		return nil
	})
}
