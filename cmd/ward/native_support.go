package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

// ssmValueResolver is native credential plumbing for the agent control plane.
// It is not an operator command surface.
func (r *Runner) ssmValueResolver(ctx context.Context, ssmPath string) (string, error) {
	out, err := r.Runner.Capture(ctx, "aws",
		"ssm", "get-parameter",
		"--name", ssmPath,
		"--with-decryption",
		"--query", "Parameter.Value",
		"--output", "text")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("ssm parameter %q returned an empty value", ssmPath)
	}
	return value, nil
}

// forgejoTokenResolver is native credential plumbing shared by issue, PR, and
// git control-plane calls. It deliberately has no dependency on AOSguard specs.
func (r *Runner) forgejoTokenResolver(ctx context.Context, ssmPath string) (string, error) {
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
	return r.ssmValueResolver(ctx, ssmPath)
}
