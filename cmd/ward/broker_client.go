package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
	"github.com/urfave/cli/v3"
)

// broker_client.go is the unprivileged side of ward's root credential broker: the
// shared client both chokepoints route through (ward#334 Unit C). See docs/broker.md.

// errBrokerUnreachable / errBrokerOutOfTier are the distinct, errors.Is-able
// failure modes; a plain error is a normal forge/API failure relayed by the broker.
var (
	errBrokerUnreachable = errors.New("broker unreachable")
	errBrokerOutOfTier   = errors.New("broker refused (write-tier only)")
)

// brokerSession is the dialled-once view of the broker for one verb: a thin wrap
// over broker.Client that classifies a round-trip into the failure modes above.
type brokerSession struct {
	client *broker.Client
}

// newBrokerSession returns a session for $WARD_BROKER_SOCK, ok=false when unset -
// the dual-mode gate every brokered path checks first.
func newBrokerSession() (*brokerSession, bool) {
	sock := strings.TrimSpace(os.Getenv(envBrokerSocket))
	if sock == "" {
		return nil, false
	}
	return &brokerSession{client: broker.NewClient(sock)}, true
}

// do sends one request and classifies the outcome: wrapped unreachable/out-of-tier
// on those modes, a plain error for a relayed API failure, else the Result.
func (s *brokerSession) do(ctx context.Context, req broker.Request) (broker.Result, error) {
	resp, err := s.client.Do(ctx, req)
	if err != nil {
		// Keep both wrapped: errors.Is(errBrokerUnreachable) holds and the
		// transport detail (broker.Client.Do already names the socket) survives.
		return broker.Result{}, fmt.Errorf("%w: %w", errBrokerUnreachable, err)
	}
	if !resp.OK {
		if isOutOfTierRefusal(resp.Error) {
			return broker.Result{}, fmt.Errorf("%w: %s", errBrokerOutOfTier, resp.Error)
		}
		return broker.Result{}, fmt.Errorf("broker: %s", resp.Error)
	}
	return resp.Result, nil
}

// isOutOfTierRefusal reports whether a Response.Error is an authorizer refusal
// (op/owner/shape gate) rather than a relayed forge error.
func isOutOfTierRefusal(msg string) bool {
	for _, marker := range []string{"not permitted", "out of scope", "out of tier", "requires", "not in allowlist", "unsupported protocol"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// forgejoReadVerbs is the read tail of `ops forgejo`: these go direct even inside
// a brokered box. Complete for the forgejo guardfile - every other verb mutates.
var forgejoReadVerbs = map[string]bool{
	"get":      true,
	"list":     true,
	"list-all": true,
	"search":   true,
	"view":     true,
}

// forgejoBrokerOp is the broker op + forced state a write leaf maps to (close and
// reopen are state edits; create/edit/comment map straight across).
type forgejoBrokerOp struct {
	op    broker.Op
	state string // forced state for close/reopen; "" leaves the request's state
}

// mapForgejoWriteOp maps an `issue` write verb to its broker op, or ok=false when
// the verb is out of the broker's write tier (delete, repo/label mutations, ...).
func mapForgejoWriteOp(resource, verbName string) (forgejoBrokerOp, bool) {
	if resource != "issue" {
		return forgejoBrokerOp{}, false
	}
	switch verbName {
	case "create":
		return forgejoBrokerOp{op: broker.OpFileIssue}, true
	case "edit":
		return forgejoBrokerOp{op: broker.OpEditIssue}, true
	case "comment":
		return forgejoBrokerOp{op: broker.OpCommentIssue}, true
	case "close":
		return forgejoBrokerOp{op: broker.OpEditIssue, state: "closed"}, true
	case "reopen":
		return forgejoBrokerOp{op: broker.OpEditIssue, state: "open"}, true
	default:
		return forgejoBrokerOp{}, false
	}
}

// verbTail splits a specverb leaf name ("ward.ops.forgejo.issue.create") into its
// classifying (resource, verb) tail; a name under two segments classifies out-of-tier.
func verbTail(name string) (resource, verbName string) {
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return "", name
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}

// brokerForgejoAction wraps a built `ops forgejo` leaf: brokered, writes route to
// the broker, out-of-tier mutations refuse, reads + --dry-run go direct (docs/broker.md).
func (r *Runner) brokerForgejoAction(name string, direct cli.ActionFunc) cli.ActionFunc {
	resource, verbName := verbTail(name)
	return func(ctx context.Context, cmd *cli.Command) error {
		session, brokered := newBrokerSession()
		if !brokered {
			return direct(ctx, cmd)
		}
		// A dry-run never fires and never touches the token, so it stays direct.
		if cmd.Bool(flagDryRun) {
			return direct(ctx, cmd)
		}
		if forgejoReadVerbs[verbName] {
			return direct(ctx, cmd)
		}
		mapped, ok := mapForgejoWriteOp(resource, verbName)
		if !ok {
			return fmt.Errorf("ward ops forgejo %s %s: refused - the broker serves the write tier only "+
				"(create / edit / comment / close / reopen issue); %s %s is out of tier", resource, verbName, resource, verbName)
		}
		return r.runForgejoWriteViaBroker(ctx, cmd, session, mapped)
	}
}

// runForgejoWriteViaBroker extracts the target + payload from the leaf's argv, sends
// the mapped op, and renders honoring the leaf's --output / --query (ward#334).
func (r *Runner) runForgejoWriteViaBroker(ctx context.Context, cmd *cli.Command, session *brokerSession, mapped forgejoBrokerOp) error {
	args := cmd.Args().Slice()
	needNumber := mapped.op != broker.OpFileIssue
	minArgs, shape := 2, "<owner> <repo>"
	if needNumber {
		minArgs, shape = 3, "<owner> <repo> <index>"
	}
	if len(args) < minArgs {
		return fmt.Errorf("ward ops forgejo: broker route needs %s, got %d arg(s)", shape, len(args))
	}
	target := broker.Target{Owner: args[0], Repo: args[1]}
	if needNumber {
		n, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("ward ops forgejo: index %q is not a number: %w", args[2], err)
		}
		target.Number = n
	}

	title, body, state, err := forgejoWritePayload(cmd)
	if err != nil {
		return err
	}
	if mapped.state != "" {
		state = mapped.state
	}

	req := broker.Request{Op: mapped.op, Target: target, Title: title, Body: body, State: state}
	res, err := session.do(ctx, req)
	if err != nil {
		return err
	}
	return renderBrokerResult(cmd, target, res)
}

// forgejoWritePayload pulls title/body/state from the body flags, falling back to a
// --body-file JSON object for fields a flag leaves unset (ward's own client shape).
func forgejoWritePayload(cmd *cli.Command) (title, body, state string, err error) {
	title = cmd.String("title")
	body = cmd.String("body")
	state = cmd.String("state")
	bodyFile := strings.TrimSpace(cmd.String("body-file"))
	if bodyFile == "" {
		return title, body, state, nil
	}
	raw, rerr := os.ReadFile(bodyFile) // #nosec G304 -- operator-supplied request body path, same as the direct leaf
	if rerr != nil {
		return "", "", "", fmt.Errorf("ward ops forgejo: read --body-file %s: %w", bodyFile, rerr)
	}
	var obj map[string]any
	if uerr := json.Unmarshal(raw, &obj); uerr != nil {
		return "", "", "", fmt.Errorf("ward ops forgejo: parse --body-file %s as JSON: %w", bodyFile, uerr)
	}
	if title == "" {
		title = stringField(obj, "title")
	}
	if body == "" {
		body = stringField(obj, "body")
	}
	if state == "" {
		state = stringField(obj, "state")
	}
	return title, body, state, nil
}

// stringField returns obj[key] when it is a string, else "".
func stringField(obj map[string]any, key string) string {
	if v, ok := obj[key].(string); ok {
		return v
	}
	return ""
}

// renderBrokerResult prints the result the way the direct leaf would: a bare number
// under --query number, a {number, html_url, url} JSON under --output json, else a ref.
func renderBrokerResult(cmd *cli.Command, target broker.Target, res broker.Result) error {
	number := res.Number
	if number == 0 {
		number = target.Number
	}
	switch {
	case cmd.String(flagQuery) == "number":
		fmt.Println(number)
	case cmd.String(flagOutput) == "json":
		out := map[string]any{"number": number}
		if res.URL != "" {
			out["html_url"] = res.URL
			out["url"] = res.URL
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return fmt.Errorf("ward ops forgejo: render broker result: %w", err)
		}
		fmt.Println(string(raw))
	default:
		ref := fmt.Sprintf("%s/%s#%d", target.Owner, target.Repo, number)
		if res.URL != "" {
			fmt.Printf("%s (via broker) %s\n", ref, res.URL)
		} else {
			fmt.Printf("%s (via broker)\n", ref)
		}
	}
	return nil
}

// brokerDispatchSeed asks the broker for target's dispatch seed (the root-held token);
// ok=false unbrokered or on any failure, so the caller falls back to env->SSM (ward#334).
func (r *Runner) brokerDispatchSeed(ctx context.Context, target broker.Target) (token string, ok bool) {
	session, brokered := newBrokerSession()
	if !brokered {
		return "", false
	}
	res, err := session.do(ctx, broker.Request{Op: broker.OpDispatch, Target: target})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ward container: broker dispatch seed unavailable (%v); falling back to the env/SSM token path (ward#334)\n", err)
		return "", false
	}
	token = strings.TrimSpace(res.Detail)
	if token == "" {
		fmt.Fprintln(os.Stderr, "ward container: broker returned an empty dispatch seed; falling back to the env/SSM token path (ward#334)")
		return "", false
	}
	return token, true
}

// planDispatchTarget builds the broker dispatch target from a child launch plan -
// its repo + issue. A seedless plan (Issue 0) is refused, falling back to env->SSM.
func planDispatchTarget(plan upPlan) broker.Target {
	return broker.Target{Owner: plan.Repo.Owner, Repo: plan.Repo.Name, Number: plan.Issue}
}
