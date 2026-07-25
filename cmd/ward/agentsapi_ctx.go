package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// agentsapi_ctx.go carves the narrow agentsapi.RunCtx / agentsapi.HostCtx views out
// of bootstrapEnv + the host Runner (ward#410); no dispatch flips. docs/agentsapi.md.

// agentHostCtx builds the launching-host view a CredentialProvider resolves
// against: GOOS + operator home, the Runner as Capture seam, blog for warnings.
func (r *Runner) agentHostCtx(ctx context.Context) agentsapi.HostCtx {
	return agentsapi.HostCtx{
		Ctx:  ctx,
		GOOS: launchHostGOOS(),
		Home: homeDir(),
		Exec: r.Runner,
		Log:  blog,
	}
}

// agentTrustDirs mirrors seed_claude_onboarding's trust set: target clone,
// /workspace root, each granted extra repo, /substrate root + warmed repos (ward#168).
func agentTrustDirs(e bootstrapEnv) []string {
	dirs := []string{primaryWorkspaceDir(containerWorkspace, targetRepo{Owner: e.TargetOwner, Name: e.TargetName}), containerWorkspace}
	for _, repo := range e.ExtraRepos {
		if repo.Name != "" {
			dirs = append(dirs, grantedRepoWorkspaceDir(containerWorkspace, repo))
		}
	}
	// Read-only context repos land under /workspace too (ward#573); trust them so
	// the agent reads them without a per-dir folder-trust re-prompt.
	for _, repo := range e.ContextRepos {
		if repo.Name != "" {
			dirs = append(dirs, grantedRepoWorkspaceDir(containerWorkspace, repo.targetRepo))
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

func harnessThreadID(mode containerMode) string {
	candidates := []string{"WARD_THREAD_ID"}
	switch mode {
	case modeCodex:
		candidates = append([]string{"CODEX_THREAD_ID"}, candidates...)
	case modeClaude:
		candidates = append([]string{"CLAUDE_SESSION_ID"}, candidates...)
	case modeGoose:
		candidates = append([]string{"GOOSE_SESSION_ID"}, candidates...)
	case modeOpencode:
		candidates = append([]string{"OPENCODE_SESSION_ID", "OPENCODE_THREAD_ID"}, candidates...)
	}
	for _, k := range candidates {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func agentCorrelation(e bootstrapEnv, mode containerMode) agentsapi.Correlation {
	ref := e.TargetOwner + "/" + e.TargetName
	if e.Issue > 0 {
		ref = ref + "#" + strconv.Itoa(e.Issue)
	}
	c := agentsapi.Correlation{
		RunID:         e.Container,
		ContainerName: e.Container,
		Role:          e.Role,
		Harness:       e.Agent,
		TargetRepo:    e.TargetOwner + "/" + e.TargetName,
		IssueRef:      ref,
		Workflow:      orDefaultLabel(os.Getenv("WARD_WORKFLOW"), "merge-remote-main"),
		ContextLevel:  e.ContextLevel,
		Version:       e.WardVersion,
		ThreadID:      harnessThreadID(mode),
	}
	if c.RunID == "" {
		c.RunID = c.ContainerName
	}
	return c
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
		ClaudeModel:    e.ClaudeModel,
		ClaudeEffort:   e.ClaudeEffort,
		GooseModel:     e.GooseModel,
		OpencodeModel:  e.OpencodeModel,
		OllamaURL:      e.OllamaURL,
		Correlation:    agentCorrelation(e, containerMode(e.Mode)),
		Seed:           seed,
		Exec:           r.Runner,
		Log:            blog,
	}
}
