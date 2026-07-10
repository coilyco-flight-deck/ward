// Package opencode is the opencode harness's agentsapi.Agent (ward#401 Phase 3,
// following ward#412). opencode drives a local Ollama-backed model (Qwen today);
// the ward#401 roster untangle renamed the mode "qwen" -> "opencode". It owns its
// config-compose + self-install behaviour directly now. See docs/agentsapi.md.
package opencode

import (
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

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
var record = agentsapi.Manifest{
	Name:         "opencode",
	Binary:       "opencode",
	ContextLevel: 0,
	Stream:       "none",
	Auth:         "none",
	Argv: agentsapi.Argv{
		Preflight:   nil,
		Headless:    []string{"opencode", "run"},
		Interactive: []string{"opencode"},
	},
	Identity: attribution.Identity{Name: "Qwen"},
}

// Agent is opencode's agentsapi.Agent. Phase 3 (ward#425) drained the behaviour
// home, so it carries no state. It resolves no host credential.
type Agent struct{}

// Compile-time proof opencode implements the core contract plus its capabilities
// (config composer + self-installer + an Ollama-reachability launch gate, ward#487).
var (
	_ agentsapi.Agent          = Agent{}
	_ agentsapi.ConfigComposer = Agent{}
	_ agentsapi.LaunchGate     = Agent{}
)

// New returns opencode's Agent.
func New() Agent { return Agent{} }

// Name is the roster key.
func (a Agent) Name() string { return record.Name }

// Record returns opencode's inert data record.
func (a Agent) Record() agentsapi.Manifest { return record }

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
func (a Agent) LaunchArgv(rc agentsapi.RunCtx) (argv []string, stream bool) {
	if rc.Headless || rc.Ask {
		return append([]string{"opencode", "run"}, rc.Seed...), false
	}
	return []string{"opencode"}, false
}
