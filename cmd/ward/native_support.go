package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

// forgejoTokenResolver is native credential plumbing shared by issue, PR, and
// git control-plane calls. It deliberately has no dependency on AOSguard specs.
func (r *Runner) forgejoTokenResolver(ctx context.Context) (string, error) {
	if os.Getenv("WARD_READONLY") == "1" {
		if token, ok := r.brokerDispatchSeed(ctx, broker.Target{Owner: brokerOwnerPrefix, Repo: "credential", Number: 1}); ok {
			return token, nil
		}
		if strings.TrimSpace(os.Getenv(envBrokerSocket)) != "" {
			return "", fmt.Errorf("forgejo credential broker is unavailable; recycle the director")
		}
	}
	if tok := strings.TrimSpace(os.Getenv("FORGEJO_TOKEN")); tok != "" {
		return tok, nil
	}
	return "", fmt.Errorf("FORGEJO_TOKEN is unset and no credential broker seed is available")
}
