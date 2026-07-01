// Package agentspi is ward's agent-agnostic contract (ward#410, Phase 1 of
// ward#401): types only, no behaviour. See docs/agentspi.md.
package agentspi

import "forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"

// Agent is the core contract every harness implements. Optional behaviour is
// split into the capability interfaces below, which core feature-tests (ward#401).
type Agent interface {
	// Name is the roster key - the --mode value, e.g. "claude".
	Name() string
	// Record returns the agent's inert data record (binary, argv, identity, ...).
	Record() Manifest
	// Signer builds the cli-guard signer from Record().Identity plus ward's marker.
	Signer() attribution.Signer
	// LaunchArgv builds the in-container agent argv (no setpriv prefix) and reports
	// whether to wrap its output in the stream-json progress parser.
	LaunchArgv(RunCtx) (argv []string, stream bool)
	// PreflightArgv builds the host GO/NO-GO one-shot argv, plus whether one exists
	// (codex/qwen have none).
	PreflightArgv(prompt string) ([]string, bool)
}

// CredentialProvider is implemented by an agent needing a host-resolved
// credential: the host resolves it into env-file lines, the container writes it.
type CredentialProvider interface {
	// ResolveCreds runs host-side, returning the env-file lines to inject.
	ResolveCreds(HostCtx) []EnvLine
	// WriteCreds runs in-container, decoding the blob into the agent's cred file.
	WriteCreds(RunCtx) error
}

// ConfigComposer is implemented by an agent that writes a provider/model config
// file in-container (codex, opencode, goose); claude needs none.
type ConfigComposer interface {
	ComposeConfig(RunCtx) error
}

// Installer is implemented by an agent whose binary is not baked into the image
// and self-installs at bootstrap (opencode).
type Installer interface {
	Install(RunCtx) error
}

// OnboardingSeeder is implemented by an agent that seeds first-run state to skip
// interactive gates (claude's ~/.claude.json onboarding + trust flags).
type OnboardingSeeder interface {
	SeedOnboarding(RunCtx) error
}

// LaunchGate is implemented by an agent with a pre-launch check that can abort
// the run (claude's auth smoke test); a failing check returns an error.
type LaunchGate interface {
	PreLaunchCheck(RunCtx) error
}
