package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

// These flag names are shared by Ward's native broker protocol. They remain
// here after the generated operator CLI moved to Aguard.
const (
	flagOutput   = "output"
	flagDryRun   = "dry-run"
	flagQuery    = "query"
	flagBodyFile = "body-file"
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
// git control-plane calls. It deliberately has no dependency on Aguard specs.
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

// captureLeafStdout is retained for the native broker result projection.
func captureLeafStdout(fn func() error) (string, error) {
	orig := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("capture stdout: %w", err)
	}
	os.Stdout = pw
	runErr := fn()
	_ = pw.Close()
	os.Stdout = orig
	out, readErr := io.ReadAll(pr)
	_ = pr.Close()
	if runErr != nil {
		return "", runErr
	}
	if readErr != nil {
		return "", fmt.Errorf("capture stdout: %w", readErr)
	}
	return string(out), nil
}
