package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// agentsapi_ctx.go carves the narrow agentsapi.RunCtx / agentsapi.HostCtx views out
// of bootstrapEnv + the host Runner (ward#410); no dispatch flips. docs/agentsapi.md.

// agentHostCtx builds the launching-host view a CredentialProvider resolves
// against: GOOS + operator home, the Runner as Capture seam, blog for warnings.
func (r *Runner) agentHostCtx(ctx context.Context) agentsapi.HostCtx {
	return agentsapi.HostCtx{
		Ctx:  ctx,
		GOOS: runtime.GOOS,
		Home: homeDir(),
		Exec: r.Runner,
		Log:  blog,
	}
}

// agentTrustDirs mirrors seed_claude_onboarding's trust set: target clone,
// /workspace root, each granted extra repo, /substrate root + warmed repos (ward#168).
func agentTrustDirs(e bootstrapEnv) []string {
	dirs := []string{"/workspace/" + e.TargetName, "/workspace"}
	for _, repo := range e.ExtraRepos {
		if repo.Name != "" {
			dirs = append(dirs, "/workspace/"+repo.Name)
		}
	}
	// Read-only context repos land under /workspace too (ward#573); trust them so
	// the agent reads them without a per-dir folder-trust re-prompt.
	for _, repo := range e.ContextRepos {
		if repo.Name != "" {
			dirs = append(dirs, "/workspace/"+repo.Name)
		}
	}
	if e.SubstrateDest != "" {
		dirs = append(dirs, e.SubstrateDest)
		if entries, err := os.ReadDir(e.SubstrateDest); err == nil {
			for _, ent := range entries {
				if ent.IsDir() {
					dirs = append(dirs, filepath.Join(e.SubstrateDest, ent.Name()))
				}
			}
		}
	}
	return dirs
}

// agentRunCtx builds the in-container view the capabilities act against; seed is
// the entrypoint's "$@" (the one-shot prompt, empty for interactive).
func (r *Runner) agentRunCtx(ctx context.Context, e bootstrapEnv, seed []string) agentsapi.RunCtx {
	return agentsapi.RunCtx{
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
