package main

import (
	"context"
	"strings"

	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agents/claude"
	"github.com/coilyco-flight-deck/ward/internal/agents/codex"
	"github.com/coilyco-flight-deck/ward/internal/agents/goose"
	"github.com/coilyco-flight-deck/ward/internal/agents/opencode"
	"github.com/coilyco-flight-deck/ward/internal/agentspi"
)

// agents_wire.go binds each SPI agent's capability closures to the still-live
// cmd/ward entrypoint funcs (ward#412, Phase 2). See docs/agentspi.md.

// envFromRunCtx rebuilds the subset of bootstrapEnv the delegated funcs read,
// tagging it with the agent's roster name/binary so their guards fire correctly.
func envFromRunCtx(name string, rc agentspi.RunCtx) bootstrapEnv {
	return bootstrapEnv{
		Mode:           name,
		Agent:          name,
		TargetName:     rc.TargetName,
		AgentHome:      rc.AgentHome,
		AgentUID:       rc.AgentUID,
		AgentGID:       rc.AgentGID,
		Headless:       rc.Headless,
		Ask:            rc.Ask,
		CodexModel:     rc.CodexModel,
		CodexEffort:    rc.CodexEffort,
		CodexVerbosity: rc.CodexVerbosity,
		QwenModel:      rc.OpencodeModel,
		OllamaURL:      rc.OllamaURL,
	}
}

// envLinesFromCredLines converts the live credEnvLines output ("KEY=VALUE") into
// the SPI's []agentspi.EnvLine, so ResolveCreds reuses the exact host resolution.
func envLinesFromCredLines(lines []string) []agentspi.EnvLine {
	out := make([]agentspi.EnvLine, 0, len(lines))
	for _, l := range lines {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		out = append(out, agentspi.EnvLine{Key: k, Value: v})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lookupAgent resolves a mode to its DATA-only registry agent, the Record()/
// Signer()/PreflightArgv() read surface; unknown -> claude (Phase 3, ward#418).
func lookupAgent(mode containerMode) agentspi.Agent {
	if a, ok := agents.Lookup(string(mode)); ok {
		return a
	}
	a, _ := agents.Lookup("claude")
	return a
}

// runCtxFromEnv builds the in-container RunCtx the capabilities + LaunchArgv read
// from the entrypoint env - the inverse of envFromRunCtx (Phase 3, ward#418).
func (r *Runner) runCtxFromEnv(ctx context.Context, e bootstrapEnv, seed []string) agentspi.RunCtx {
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

// composeAgentContainer runs the in-container setup capabilities feature-tested,
// keeping the old creds -> onboarding -> config order (Phase 3, ward#418).
func composeAgentContainer(agent agentspi.Agent, rc agentspi.RunCtx) {
	if cp, ok := agent.(agentspi.CredentialProvider); ok {
		_ = cp.WriteCreds(rc)
	}
	if seeder, ok := agent.(agentspi.OnboardingSeeder); ok {
		_ = seeder.SeedOnboarding(rc)
	}
	if cc, ok := agent.(agentspi.ConfigComposer); ok {
		_ = cc.ComposeConfig(rc)
	}
}

// wireAgent returns the registry agent for mode with its capability closures
// bound to this Runner's live funcs; unknown modes mirror agents.Lookup.
func (r *Runner) wireAgent(mode containerMode) (agentspi.Agent, bool) {
	switch mode {
	case modeClaude:
		return claude.Agent{
			ResolveCredsFn: func(hc agentspi.HostCtx) []agentspi.EnvLine {
				return envLinesFromCredLines(credEnvLines(r.resolveAgentCreds(hc.Ctx, modeClaude)))
			},
			WriteCredsFn:     func(rc agentspi.RunCtx) error { r.writeClaudeCreds(envFromRunCtx("claude", rc)); return nil },
			SeedOnboardingFn: func(rc agentspi.RunCtx) error { r.seedClaudeOnboarding(envFromRunCtx("claude", rc)); return nil },
			PreLaunchCheckFn: func(rc agentspi.RunCtx) error { return r.smokeTestClaudeAuth(rc.Ctx, envFromRunCtx("claude", rc)) },
		}, true
	case modeCodex:
		return codex.Agent{
			ResolveCredsFn: func(hc agentspi.HostCtx) []agentspi.EnvLine {
				return envLinesFromCredLines(credEnvLines(r.resolveAgentCreds(hc.Ctx, modeCodex)))
			},
			WriteCredsFn:    func(rc agentspi.RunCtx) error { r.writeCodexCreds(envFromRunCtx("codex", rc)); return nil },
			ComposeConfigFn: func(rc agentspi.RunCtx) error { r.composeCodexConfig(envFromRunCtx("codex", rc)); return nil },
		}, true
	case modeOpencode:
		return opencode.Agent{
			ComposeConfigFn: func(rc agentspi.RunCtx) error { r.composeOpencodeConfig(envFromRunCtx("opencode", rc)); return nil },
			InstallFn:       func(rc agentspi.RunCtx) error { r.installOpencode(rc.Ctx, envFromRunCtx("opencode", rc)); return nil },
		}, true
	case modeGoose:
		return goose.Agent{
			ComposeConfigFn: func(rc agentspi.RunCtx) error { r.composeGooseConfig(envFromRunCtx("goose", rc)); return nil },
		}, true
	default:
		return agents.Lookup(string(mode))
	}
}
