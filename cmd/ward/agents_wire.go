package main

import (
	"github.com/coilyco-flight-deck/ward/internal/agents"
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"
)

// agents_wire.go holds thin core-side dispatch helpers over agentsapi.
// Per-agent bodies live in their folders; see docs/agent-harnesses.md.

// lookupAgent resolves a mode to its registry agent (data + behaviour); an
// unknown mode falls back to claude, matching the switches' default arm.
func lookupAgent(mode containerMode) agentsapi.Agent {
	if a, ok := agents.Lookup(string(mode)); ok {
		return a
	}
	a, _ := agents.Lookup(string(modeClaude))
	return a
}

// localModelAgent reports a self-hosted / local model vs a trusted cloud credential
// (agents.LocalModel keeps the per-agent auth literals out of core; ward#162).
func localModelAgent(rec agentsapi.Manifest) bool {
	return agents.LocalModel(rec)
}

// hostOneShotTrusted reports whether a harness may run one of ward's unsandboxed HOST
// one-shot reads; local-model harnesses are barred (ward#162, docs/agent-lifecycle.md).
func hostOneShotTrusted(mode containerMode) bool {
	return !localModelAgent(lookupAgent(mode).Record())
}

// hostOneShotArgv is the single chokepoint for a HOST one-shot argv: a local-model
// harness is refused (ok=false) up front, else the registry preflight argv (ward#162).
func hostOneShotArgv(mode containerMode, prompt string) ([]string, bool) {
	if !hostOneShotTrusted(mode) {
		return nil, false
	}
	return lookupAgent(mode).PreflightArgv(prompt)
}

// hostOneShot splits a trusted host one-shot into a prompt-free base argv plus the
// prompt to feed on stdin, off the command line (ward#548); ok mirrors hostOneShotArgv.
func hostOneShot(mode containerMode, prompt string) (argv []string, stdin string, ok bool) {
	full, ok := hostOneShotArgv(mode, prompt)
	if !ok {
		return nil, "", false
	}
	// PreflightArgv appends the prompt last; lift it onto stdin so only the base
	// command rides the command line (claude -p reads its stdin as the prompt).
	return full[:len(full)-1], prompt, true
}

// composeAgentContainer runs only the selected adapter's in-container setup
// capabilities. An adapter without a capability receives none of its files.
func composeAgentContainer(agent agentsapi.Agent, rc agentsapi.RunCtx) {
	if cp, ok := agent.(agentsapi.CredentialProvider); ok {
		_ = cp.WriteCreds(rc)
	}
	if pc, ok := agent.(agentsapi.PermissionComposer); ok {
		_ = pc.ComposePermissions(rc)
	}
	if seeder, ok := agent.(agentsapi.OnboardingSeeder); ok {
		_ = seeder.SeedOnboarding(rc)
	}
	if cc, ok := agent.(agentsapi.ConfigComposer); ok {
		_ = cc.ComposeConfig(rc)
	}
}
