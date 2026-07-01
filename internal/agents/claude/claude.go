// Package claude is the claude harness's agentspi.Agent (ward#412, Phase 2 of
// ward#401). It carries claude's inert data record and forwards its capability
// behaviour to the still-live cmd/ward funcs via closures core injects; no
// behaviour lives here. See docs/agentspi.md.
package claude

import (
	"github.com/coilyco-flight-deck/ward/internal/agentspi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"
)

// signature policy shared with cmd/ward's agentSigner; the contract test pins
// Signer() to the live switch so these two copies can never drift silently.
const (
	signatureMarker = "<!-- ward-agent-signature -->"
	signatureVia    = "via `ward agent`"
	signerEmail     = "claude@ward.agent"
)

// record is claude's inert data record, mirroring the agent-adapter manifest and
// the cmd/ward switches. See docs/agentspi.md.
var record = agentspi.Manifest{
	Name:         "claude",
	Binary:       "claude",
	ContextLevel: 2,
	Stream:       "stream-json",
	Auth:         "claude-keychain",
	Argv: agentspi.Argv{
		Preflight:   []string{"claude", "-p"},
		Headless:    []string{"claude", "-p", "--verbose", "--output-format", "stream-json"},
		Interactive: []string{"claude"},
	},
	Identity: attribution.Identity{Name: "Claude", Pronouns: "she/her"},
}

// Agent is claude's agentspi.Agent; core injects the capability closures so the
// behaviour keeps living in the entrypoint funcs. A nil closure is a safe no-op.
type Agent struct {
	ResolveCredsFn   func(agentspi.HostCtx) []agentspi.EnvLine // -> resolveClaudeCreds + credEnvLines
	WriteCredsFn     func(agentspi.RunCtx) error               // -> writeClaudeCreds
	SeedOnboardingFn func(agentspi.RunCtx) error               // -> seedClaudeOnboarding
	PreLaunchCheckFn func(agentspi.RunCtx) error               // -> smokeTestClaudeAuth
}

// Compile-time proof claude implements the core contract plus exactly the
// capabilities it supports (credentials, onboarding seed, launch gate).
var (
	_ agentspi.Agent              = Agent{}
	_ agentspi.CredentialProvider = Agent{}
	_ agentspi.OnboardingSeeder   = Agent{}
	_ agentspi.LaunchGate         = Agent{}
)

// New returns claude's Agent with no capabilities wired (DATA-only); core wires
// the closures at dispatch.
func New() Agent { return Agent{} }

// Name is the roster key (the --mode value).
func (a Agent) Name() string { return record.Name }

// Record returns claude's inert data record.
func (a Agent) Record() agentspi.Manifest { return record }

// Signer builds claude's cli-guard signer from its identity plus ward's marker,
// footer tail, and Co-Authored-By email. Mirrors cmd/ward's agentSigner.
func (a Agent) Signer() attribution.Signer {
	return attribution.Signer{
		Identity: record.Identity,
		Marker:   signatureMarker,
		Via:      signatureVia,
		Email:    signerEmail,
	}
}

// PreflightArgv returns claude's host one-shot argv with the prompt appended.
func (a Agent) PreflightArgv(prompt string) ([]string, bool) {
	return append(append([]string{}, record.Argv.Preflight...), prompt), true
}

// LaunchArgv builds the in-container claude argv (no setpriv prefix) and reports
// whether to stream-wrap its output. Mirrors cmd/ward's buildAgentArgv default.
func (a Agent) LaunchArgv(rc agentspi.RunCtx) (argv []string, stream bool) {
	argv = []string{record.Binary}
	switch {
	case rc.Ask:
		argv = append(argv, "-p")
	case rc.Headless:
		argv = append(argv, "-p", "--verbose", "--output-format", "stream-json")
		stream = true
	}
	argv = append(argv, rc.Seed...)
	return argv, stream
}

// ResolveCreds runs host-side, returning the env-file lines to inject.
func (a Agent) ResolveCreds(hc agentspi.HostCtx) []agentspi.EnvLine {
	if a.ResolveCredsFn == nil {
		return nil
	}
	return a.ResolveCredsFn(hc)
}

// WriteCreds runs in-container, decoding the blob into ~/.claude/.credentials.json.
func (a Agent) WriteCreds(rc agentspi.RunCtx) error {
	if a.WriteCredsFn == nil {
		return nil
	}
	return a.WriteCredsFn(rc)
}

// SeedOnboarding seeds ~/.claude.json so interactive claude skips its first-run gates.
func (a Agent) SeedOnboarding(rc agentspi.RunCtx) error {
	if a.SeedOnboardingFn == nil {
		return nil
	}
	return a.SeedOnboardingFn(rc)
}

// PreLaunchCheck runs claude's auth smoke test, which can abort the run.
func (a Agent) PreLaunchCheck(rc agentspi.RunCtx) error {
	if a.PreLaunchCheckFn == nil {
		return nil
	}
	return a.PreLaunchCheckFn(rc)
}
