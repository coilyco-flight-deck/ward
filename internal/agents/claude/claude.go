// Package claude is the claude harness's agentsapi.Agent (ward#401 Phase 3,
// following ward#412). It owns claude's inert data record AND its capability
// behaviour: credential resolve/write, onboarding seed, and the pre-launch auth
// smoke test all live here now, not behind a closure into core. See
// docs/agentsapi.md.
package claude

import (
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

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
// the cmd/ward switches. See docs/agentsapi.md.
var record = agentsapi.Manifest{
	Name:         "claude",
	Binary:       "claude",
	ContextLevel: 2,
	Stream:       "stream-json",
	Auth:         "claude-keychain",
	StatusLine:   true,
	Argv: agentsapi.Argv{
		Preflight:   []string{"claude", "-p"},
		Headless:    []string{"claude", "-p", "--verbose", "--output-format", "stream-json"},
		Interactive: []string{"claude"},
	},
	Identity: attribution.Identity{Name: "Claude", Pronouns: "she/her"},
}

// Agent is claude's agentsapi.Agent; Phase 3 (ward#425) drained the behaviour
// home, so it carries no state (methods act on the passed RunCtx/HostCtx).
type Agent struct{}

// Compile-time proof claude implements the core contract plus exactly the
// capabilities it supports (credentials, onboarding seed, launch gate).
var (
	_ agentsapi.Agent              = Agent{}
	_ agentsapi.CredentialProvider = Agent{}
	_ agentsapi.OnboardingSeeder   = Agent{}
	_ agentsapi.LaunchGate         = Agent{}
)

// New returns claude's Agent.
func New() Agent { return Agent{} }

// Name is the roster key (the --mode value).
func (a Agent) Name() string { return record.Name }

// Record returns claude's inert data record.
func (a Agent) Record() agentsapi.Manifest { return record }

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
func (a Agent) LaunchArgv(rc agentsapi.RunCtx) (argv []string, stream bool) {
	argv = []string{record.Binary}
	switch {
	case rc.Ask:
		argv = append(argv, "-p")
	case rc.Headless:
		argv = append(argv, "-p", "--verbose", "--output-format", "stream-json")
		stream = true
	}
	// --model rides only when resolved (ward#616); empty keeps today's bare launch.
	// Effort has no native claude flag, so rc.ClaudeEffort is echo-only, not argv.
	if rc.ClaudeModel != "" {
		argv = append(argv, "--model", rc.ClaudeModel)
	}
	argv = append(argv, rc.Seed...)
	return argv, stream
}
