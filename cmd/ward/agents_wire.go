package main

import (
	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// agents_wire.go holds the thin core-side dispatch helpers over the agentsapi
// seam; the per-agent bodies live in their folders now (ward#425). docs/agentsapi.md.

// lookupAgent resolves a mode to its registry agent (data + behaviour); an
// unknown mode falls back to claude, matching the switches' default arm.
func lookupAgent(mode containerMode) agentsapi.Agent {
	if a, ok := agents.Lookup(string(mode)); ok {
		return a
	}
	a, _ := agents.Lookup(string(modeClaude))
	return a
}

// composeAgentContainer runs the in-container setup capabilities feature-tested,
// keeping the creds -> onboarding -> config order (ward#425).
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
