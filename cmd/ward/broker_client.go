package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
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
