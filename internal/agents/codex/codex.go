// Package codex is the codex harness's agentspi.Agent (ward#412, Phase 2 of
// ward#401). It carries codex's inert data record and forwards its capability
// behaviour to the still-live cmd/ward funcs via closures core injects; no
// behaviour lives here. See docs/agentspi.md.
package codex

import (
	"github.com/coilyco-flight-deck/ward/internal/agentspi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"
)

const (
	signatureMarker = "<!-- ward-agent-signature -->"
	signatureVia    = "via `ward agent`"
	signerEmail     = "codex@ward.agent"
)

// record mirrors the agent-adapter manifest + the cmd/ward switches: codex is a
// scoped-context harness with no host pre-flight and a plain `codex exec` headless.
var record = agentspi.Manifest{
	Name:         "codex",
	Binary:       "codex",
	ContextLevel: 1,
	Stream:       "none",
	Auth:         "codex-file",
	Argv: agentspi.Argv{
		Preflight:   nil,
		Headless:    []string{"codex", "exec"},
		Interactive: []string{"codex"},
	},
	Identity: attribution.Identity{Name: "Codex"},
}

// Agent is codex's agentspi.Agent; core injects the capability closures
// (writeCodexCreds, resolveCodexCreds, composeCodexConfig).
type Agent struct {
	ResolveCredsFn  func(agentspi.HostCtx) []agentspi.EnvLine // -> resolveCodexCreds + credEnvLines
	WriteCredsFn    func(agentspi.RunCtx) error               // -> writeCodexCreds
	ComposeConfigFn func(agentspi.RunCtx) error               // -> composeCodexConfig
}

// Compile-time proof codex implements the core contract plus its capabilities.
var (
	_ agentspi.Agent              = Agent{}
	_ agentspi.CredentialProvider = Agent{}
	_ agentspi.ConfigComposer     = Agent{}
)

// New returns codex's Agent with no capabilities wired (DATA-only).
func New() Agent { return Agent{} }

// Name is the roster key.
func (a Agent) Name() string { return record.Name }

// Record returns codex's inert data record.
func (a Agent) Record() agentspi.Manifest { return record }

// Signer builds codex's cli-guard signer; mirrors cmd/ward's agentSigner.
func (a Agent) Signer() attribution.Signer {
	return attribution.Signer{
		Identity: record.Identity,
		Marker:   signatureMarker,
		Via:      signatureVia,
		Email:    signerEmail,
	}
}

// PreflightArgv reports codex has no host one-shot pre-flight yet.
func (a Agent) PreflightArgv(string) ([]string, bool) { return nil, false }

// LaunchArgv builds codex's in-container argv; mirrors cmd/ward's buildAgentArgv.
func (a Agent) LaunchArgv(rc agentspi.RunCtx) (argv []string, stream bool) {
	if rc.Headless || rc.Ask {
		return append([]string{"codex", "exec"}, rc.Seed...), false
	}
	return append([]string{"codex"}, rc.Seed...), false
}

// ResolveCreds runs host-side, returning the env-file lines to inject.
func (a Agent) ResolveCreds(hc agentspi.HostCtx) []agentspi.EnvLine {
	if a.ResolveCredsFn == nil {
		return nil
	}
	return a.ResolveCredsFn(hc)
}

// WriteCreds decodes the host-injected auth.json into ~/.codex/auth.json.
func (a Agent) WriteCreds(rc agentspi.RunCtx) error {
	if a.WriteCredsFn == nil {
		return nil
	}
	return a.WriteCredsFn(rc)
}

// ComposeConfig writes codex's approvals-off/sandbox-open config in-container.
func (a Agent) ComposeConfig(rc agentspi.RunCtx) error {
	if a.ComposeConfigFn == nil {
		return nil
	}
	return a.ComposeConfigFn(rc)
}
