package main

import (
	"context"
	"runtime"

	"github.com/coilyco-flight-deck/ward/internal/agentspi"
)

// agentspi_ctx.go carves the narrow agentspi.RunCtx / agentspi.HostCtx views out
// of bootstrapEnv + the host Runner (ward#410); no dispatch flips. docs/agentspi.md.

// agentHostCtx builds the launching-host view a CredentialProvider resolves
// against: GOOS + operator home, the Runner as Capture seam, blog for warnings.
func (r *Runner) agentHostCtx(ctx context.Context) agentspi.HostCtx {
	return agentspi.HostCtx{
		Ctx:  ctx,
		GOOS: runtime.GOOS,
		Home: homeDir(),
		Exec: r.Runner,
		Log:  blog,
	}
}

// agentRunCtx builds the in-container view the capabilities act against; seed is
// the entrypoint's "$@" (the one-shot prompt, empty for interactive).
func (r *Runner) agentRunCtx(ctx context.Context, e bootstrapEnv, seed []string) agentspi.RunCtx {
	return agentspi.RunCtx{
		Ctx:            ctx,
		AgentHome:      e.AgentHome,
		TargetName:     e.TargetName,
		AgentUID:       e.AgentUID,
		AgentGID:       e.AgentGID,
		Headless:       e.Headless,
		Ask:            e.Ask,
		CodexModel:     e.CodexModel,
		CodexEffort:    e.CodexEffort,
		CodexVerbosity: e.CodexVerbosity,
		OpencodeModel:  e.QwenModel,
		OllamaURL:      e.OllamaURL,
		Seed:           seed,
		Exec:           r.Runner,
		Log:            blog,
	}
}
