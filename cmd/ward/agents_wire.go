package main

import (
	"context"

	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agents/goose"
	"github.com/coilyco-flight-deck/ward/internal/agents/opencode"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// agents_wire.go binds each SPI agent's capability closures to the still-live
// cmd/ward entrypoint funcs (ward#412, Phase 2). See docs/agentsapi.md.

// envFromRunCtx rebuilds the subset of bootstrapEnv the delegated funcs read,
// tagging it with the agent's roster name/binary so their guards fire correctly.
func envFromRunCtx(name string, rc agentsapi.RunCtx) bootstrapEnv {
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

// lookupAgent resolves a mode to its DATA-only registry agent, the Record()/
// Signer()/PreflightArgv() read surface; unknown -> claude (Phase 3, ward#418).
func lookupAgent(mode containerMode) agentsapi.Agent {
	if a, ok := agents.Lookup(string(mode)); ok {
		return a
	}
	a, _ := agents.Lookup("claude")
	return a
}

// runCtxFromEnv builds the in-container RunCtx the capabilities + LaunchArgv read
// from the entrypoint env - the inverse of envFromRunCtx (Phase 3, ward#418).
func (r *Runner) runCtxFromEnv(ctx context.Context, e bootstrapEnv, seed []string) agentsapi.RunCtx {
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

// composeAgentContainer runs the in-container setup capabilities feature-tested,
// keeping the old creds -> onboarding -> config order (Phase 3, ward#418).
func composeAgentContainer(agent agentsapi.Agent, rc agentsapi.RunCtx) {
	if cp, ok := agent.(agentsapi.CredentialProvider); ok {
		_ = cp.WriteCreds(rc)
	}
	if seeder, ok := agent.(agentsapi.OnboardingSeeder); ok {
		_ = seeder.SeedOnboarding(rc)
	}
	if cc, ok := agent.(agentsapi.ConfigComposer); ok {
		_ = cc.ComposeConfig(rc)
	}
}

// wireAgent returns the registry agent for mode with its capability closures
// bound to this Runner's live funcs; unknown modes mirror agents.Lookup.
func (r *Runner) wireAgent(mode containerMode) (agentsapi.Agent, bool) {
	switch mode {
	case modeClaude, modeCodex:
		// Drained to their folders (ward#425): the registry agent owns the behaviour.
		return agents.Lookup(string(mode))
	case modeOpencode:
		return opencode.Agent{
			ComposeConfigFn: func(rc agentsapi.RunCtx) error { r.composeOpencodeConfig(envFromRunCtx("opencode", rc)); return nil },
			InstallFn:       func(rc agentsapi.RunCtx) error { r.installOpencode(rc.Ctx, envFromRunCtx("opencode", rc)); return nil },
		}, true
	case modeGoose:
		return goose.Agent{
			ComposeConfigFn: func(rc agentsapi.RunCtx) error { r.composeGooseConfig(envFromRunCtx("goose", rc)); return nil },
		}, true
	default:
		return agents.Lookup(string(mode))
	}
}
