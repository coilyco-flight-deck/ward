// Package agentsapi is ward's agent-agnostic contract (ward#410, Phase 1 of
// ward#401): types only, no behaviour. See docs/agentsapi.md.
package agentsapi

import "forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"

// Agent is the core contract every harness implements. Install is required so
// bootstrap can prove the harness is ready before launch or fail loudly.
type Agent interface {
	// Name is the roster key - the --mode value, e.g. "claude".
	Name() string
	// Record returns the agent's inert data record (binary, argv, identity, ...).
	Record() Manifest
	// Signer builds the cli-guard signer from Record().Identity plus ward's marker.
	Signer() attribution.Signer
	// Install performs the harness bootstrap step before launch. Self-contained
	// harnesses can make this a verified no-op.
	Install(RunCtx) error
	// LaunchArgv builds the in-container agent argv (no setpriv prefix) and reports
	// whether to wrap its output in the stream-json progress parser.
	LaunchArgv(RunCtx) (argv []string, stream bool)
	// PreflightArgv builds the host GO/NO-GO one-shot argv, plus whether one exists
	// (codex/opencode have none).
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

// OnboardingSeeder is implemented by an agent that seeds first-run state to skip
// interactive gates (claude's ~/.claude.json onboarding + trust flags).
type OnboardingSeeder interface {
	SeedOnboarding(RunCtx) error
}

// LaunchGate is implemented by an agent with a pre-launch check that can abort
// the run (claude's auth smoke test); a failing check returns an error or GateError.
type LaunchGate interface {
	PreLaunchCheck(RunCtx) error
}

// GateError names a specific pre-launch recovery path, such as model-config,
// while still carrying the underlying error detail.
type GateError struct {
	Gate string
	Err  error
}

// Error returns the wrapped error text.
func (e GateError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying error for errors.Is/errors.As.
func (e GateError) Unwrap() error { return e.Err }

// GateName reports the named recovery path.
func (e GateError) GateName() string { return e.Gate }

// NewGateError wraps err with a named recovery gate.
func NewGateError(gate string, err error) error {
	if err == nil {
		return nil
	}
	return GateError{Gate: gate, Err: err}
}
