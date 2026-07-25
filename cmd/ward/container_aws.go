package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// container_aws.go delivers AWS creds by exporting the host's credential chain and
// injecting it as AWS_* env, mount ~/.aws the fallback. See docs/agent-aws-creds.md.

// AWS_* env keys the export path injects; the SDK reads these ahead of any ~/.aws
// profile, so the run gets working creds with zero chain replication (ward#586).
const (
	awsAccessKeyEnv     = "AWS_ACCESS_KEY_ID"
	awsSecretKeyEnv     = "AWS_SECRET_ACCESS_KEY" // #nosec G101 -- env var name, not a secret
	awsSessionTokenEnv  = "AWS_SESSION_TOKEN"     // #nosec G101 -- env var name, not a secret
	awsDefaultRegionEnv = "AWS_DEFAULT_REGION"
	awsRegionEnv        = "AWS_REGION"
)

// awsExportedCreds is the flat JSON `aws configure export-credentials --format process`
// returns (same shape on every OS); Expiration is empty for static IAM-user keys.
type awsExportedCreds struct {
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken"`
	Expiration      string `json:"Expiration"`
}

// awsExportCredsArgv is the host argv resolving the whole credential chain to flat JSON;
// `aws` resolves cross-platform (aws.exe on Windows) like ward's other host-side reads.
func awsExportCredsArgv() []string {
	return []string{"configure", "export-credentials", "--format", "process"}
}

// parseAWSExportCreds parses the export-credentials JSON, requiring the access-key pair;
// a session token is optional (static IAM-user keys carry none). Pure + testable.
func parseAWSExportCreds(out []byte) (awsExportedCreds, error) {
	var c awsExportedCreds
	if err := json.Unmarshal(out, &c); err != nil {
		return awsExportedCreds{}, fmt.Errorf("parse export-credentials JSON: %w", err)
	}
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return awsExportedCreds{}, fmt.Errorf("export-credentials returned no access key")
	}
	return c, nil
}

// envLines renders the resolved creds (+ optional region) as env-file lines; the session
// token and region ride only when present, so static keys ship no empty vars. Pure.
func (c awsExportedCreds) envLines(region string) []agentsapi.EnvLine {
	lines := []agentsapi.EnvLine{
		{Key: awsAccessKeyEnv, Value: c.AccessKeyID},
		{Key: awsSecretKeyEnv, Value: c.SecretAccessKey},
	}
	if c.SessionToken != "" {
		lines = append(lines, agentsapi.EnvLine{Key: awsSessionTokenEnv, Value: c.SessionToken})
	}
	// Dropping the mount removes ~/.aws/config as a region source, so inject the host's.
	if region != "" {
		lines = append(lines,
			agentsapi.EnvLine{Key: awsDefaultRegionEnv, Value: region},
			agentsapi.EnvLine{Key: awsRegionEnv, Value: region})
	}
	return lines
}

// awsExpiryNote is the launch heads-up naming the injected creds' expiry; a run outliving
// it loses AWS access (re-export deferred - provision runs are minutes; ward#586). Pure.
func awsExpiryNote(expiration string, now time.Time) string {
	expiration = strings.TrimSpace(expiration)
	if expiration == "" {
		return "ward container: injected host AWS credentials (no expiry - static keys; ward#586)"
	}
	msg := "ward container: injected host AWS credentials; they expire " + expiration +
		" - a run outliving that loses AWS access (ward#586)"
	if exp, err := time.Parse(time.RFC3339, expiration); err == nil {
		msg += fmt.Sprintf(" (%s from now)", exp.Sub(now).Round(time.Second))
	}
	return msg
}

// resolveLaunchCreds resolves the env-file lines a launch injects: the mode's agent
// creds plus, when the aws capability is on, the exported AWS creds (may mutate plan).
func (r *Runner) resolveLaunchCreds(ctx context.Context, plan *upPlan, mode containerMode) []agentsapi.EnvLine {
	creds := r.resolveAgentCreds(ctx, mode)
	return append(creds, r.resolveAWSInjectCreds(ctx, plan)...)
}

// resolveDirectorStackCreds host-resolves every harness before broker launch.
// Child launches reuse those channels without access to host credential stores.
func (r *Runner) resolveDirectorStackCreds(ctx context.Context, plan *upPlan, directorMode containerMode) []agentsapi.EnvLine {
	modes := make([]containerMode, 0, len(agentModes))
	modes = append(modes, directorMode)
	for _, mode := range agentModes {
		if mode != directorMode {
			modes = append(modes, mode)
		}
	}

	seen := make(map[string]bool)
	creds := make([]agentsapi.EnvLine, 0, len(modes))
	appendUnique := func(lines []agentsapi.EnvLine) {
		for _, line := range lines {
			if line.Key == "" || seen[line.Key] {
				continue
			}
			seen[line.Key] = true
			creds = append(creds, line)
		}
	}
	for _, mode := range modes {
		appendUnique(r.resolveAgentCreds(ctx, mode))
	}
	appendUnique(r.resolveAWSInjectCreds(ctx, plan))
	return creds
}

// resolveAWSInjectCreds lets broker children reuse inherited AWS via CLI export
// (ward#586). See docs/agent-aws-creds.md for the export/fallback.
func (r *Runner) resolveAWSInjectCreds(ctx context.Context, plan *upPlan) []agentsapi.EnvLine {
	if plan.AWSHome == "" {
		return nil // aws capability off - nothing to inject
	}
	out, err := r.Runner.Capture(ctx, "aws", awsExportCredsArgv()...)
	if err != nil {
		// AWS CLI v2 absent, or the chain resolved nothing: keep the mount fallback.
		blog("ward container: aws export-credentials failed (%v); falling back to ~/.aws mount (ward#586)", err)
		return nil
	}
	creds, perr := parseAWSExportCreds(out)
	if perr != nil {
		blog("ward container: %v; falling back to the ~/.aws mount (ward#586)", perr)
		return nil
	}
	region := r.ResolveAWSRegion(ctx)
	// Export succeeded: env supersedes the mount, so drop it (clearing AWSHome silences the
	// #579 empty-mount warning - the real "no creds" signal is now an export failure).
	plan.dropAWSMount()
	blog(awsExpiryNote(creds.Expiration, time.Now()))
	return creds.envLines(region)
}

// resolveAWSRegion resolves the region to inject alongside exported creds (export gives
// none): host AWS_REGION / AWS_DEFAULT_REGION, else the configured default.
func (r *Runner) ResolveAWSRegion(ctx context.Context) string {
	if v := strings.TrimSpace(os.Getenv(awsRegionEnv)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(awsDefaultRegionEnv)); v != "" {
		return v
	}
	out, err := r.Runner.Capture(ctx, "aws", "configure", "get", "region")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// dropAWSMount removes the read-only ~/.aws bind (target containerAWSMount) and clears
// AWSHome so a successful export fully supersedes the mount (ward#586).
func (p *upPlan) dropAWSMount() {
	kept := make([]mountSpec, 0, len(p.Mounts))
	for _, m := range p.Mounts {
		if m.Target == containerAWSMount {
			continue
		}
		kept = append(kept, m)
	}
	p.Mounts = kept
	p.AWSHome = ""
}
