// Package opencode is the opencode harness's agentspi.Agent (ward#412, Phase 2
// of ward#401). opencode drives a local ollama-backed model (qwen today); the
// ward#401 roster untangle renamed the mode "qwen" -> "opencode" so the roster
// key names the harness, not its backing model. It forwards its capability
// behaviour to the still-live cmd/ward funcs via closures core injects; no
// behaviour lives here. See docs/agentspi.md.
package opencode

import (
	"github.com/coilyco-flight-deck/ward/internal/agentspi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"
)

const (
	signatureMarker = "<!-- ward-agent-signature -->"
	signatureVia    = "via `ward agent`"
	// signerEmail follows the renamed mode; the identity keeps the "Qwen" persona
	// (the backing model), matching cmd/ward's agentIdentity.
	signerEmail = "opencode@ward.agent"
)

// record mirrors the agent-adapter manifest + the cmd/ward switches: opencode is
// the minimal-context floor, needs no host credential (local ollama).
var record = agentspi.Manifest{
	Name:         "opencode",
	Binary:       "opencode",
	ContextLevel: 0,
	Stream:       "none",
	Auth:         "none",
	Argv: agentspi.Argv{
		Preflight:   nil,
		Headless:    []string{"opencode", "run"},
		Interactive: []string{"opencode"},
	},
	Identity: attribution.Identity{Name: "Qwen"},
}

// Agent is opencode's agentspi.Agent; core injects the capability closures
// (composeOpencodeConfig, installOpencode). It resolves no host credential.
type Agent struct {
	ComposeConfigFn func(agentspi.RunCtx) error // -> composeOpencodeConfig
	InstallFn       func(agentspi.RunCtx) error // -> installOpencode
}

// Compile-time proof opencode implements the core contract plus its capabilities
// (config composer + self-installer). It is deliberately not a CredentialProvider.
var (
	_ agentspi.Agent          = Agent{}
	_ agentspi.ConfigComposer = Agent{}
	_ agentspi.Installer      = Agent{}
)

// New returns opencode's Agent with no capabilities wired (DATA-only).
func New() Agent { return Agent{} }

// Name is the roster key.
func (a Agent) Name() string { return record.Name }

// Record returns opencode's inert data record.
func (a Agent) Record() agentspi.Manifest { return record }

// Signer builds opencode's cli-guard signer; mirrors cmd/ward's agentSigner.
func (a Agent) Signer() attribution.Signer {
	return attribution.Signer{
		Identity: record.Identity,
		Marker:   signatureMarker,
		Via:      signatureVia,
		Email:    signerEmail,
	}
}

// PreflightArgv reports opencode has no host one-shot pre-flight (ollama is local).
func (a Agent) PreflightArgv(string) ([]string, bool) { return nil, false }

// LaunchArgv builds opencode's in-container argv; mirrors cmd/ward's buildAgentArgv.
// Interactive drops the seed (the opencode TUI is not auto-fed a prompt).
func (a Agent) LaunchArgv(rc agentspi.RunCtx) (argv []string, stream bool) {
	if rc.Headless || rc.Ask {
		return append([]string{"opencode", "run"}, rc.Seed...), false
	}
	return []string{"opencode"}, false
}

// ComposeConfig writes the ollama-backed opencode config in-container.
func (a Agent) ComposeConfig(rc agentspi.RunCtx) error {
	if a.ComposeConfigFn == nil {
		return nil
	}
	return a.ComposeConfigFn(rc)
}

// Install self-installs the opencode binary (absent from the image).
func (a Agent) Install(rc agentspi.RunCtx) error {
	if a.InstallFn == nil {
		return nil
	}
	return a.InstallFn(rc)
}
