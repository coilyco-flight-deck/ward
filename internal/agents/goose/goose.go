// Package goose is the goose harness's agentsapi.Agent (ward#401 Phase 3,
// following ward#412). It owns goose's inert data record AND its config-compose
// behaviour, including provider, model, and host-resolved endpoint. See
// docs/agent-harnesses.md.
package goose

import (
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/attribution"
)

const (
	signatureMarker = "<!-- ward-agent-signature -->"
	signatureVia    = "via `ward agent`"
	signerEmail     = "goose@ward.agent"
)

// record mirrors the agent-adapter manifest + the cmd/ward switches; goose's
// ollama endpoint is composed into config, so it is a ConfigComposer only.
var record = agentsapi.Manifest{
	Name:         "goose",
	Binary:       "goose",
	ContextLevel: 2,
	Stream:       "none",
	Auth:         "ollama",
	Projection: agentsapi.Projection{
		InstructionSources: []string{".goosehints"},
		InstructionPath:    ".config/goose/.goosehints",
		SkillsPath:         ".agents/skills",
		ConfigPaths:        []string{".config/goose/config.yaml"},
		StatePaths:         []string{".config/goose"},
		OwnershipPaths:     []string{".config/goose", ".agents"},
	},
	Argv: agentsapi.Argv{
		Preflight:   []string{"goose", "run", "-t"},
		Headless:    []string{"goose", "run", "--no-session", "-t"},
		Interactive: []string{"goose", "session"},
	},
	Identity: attribution.Identity{Name: "Goose"},
}

// Agent is goose's agentsapi.Agent. Phase 3 (ward#425) drained the behaviour
// home, so it carries no state.
type Agent struct{}

// Compile-time proof goose implements the core contract plus its capabilities:
// config compose + a pre-launch Ollama-reachability gate (ward#487).
var (
	_ agentsapi.Agent          = Agent{}
	_ agentsapi.ConfigComposer = Agent{}
	_ agentsapi.LaunchGate     = Agent{}
)

// New returns goose's Agent.
func New() Agent { return Agent{} }

// Name is the roster key.
func (a Agent) Name() string { return record.Name }

// Record returns goose's inert data record.
func (a Agent) Record() agentsapi.Manifest { return record }

// Signer builds goose's umbra signer; mirrors cmd/ward's agentSigner.
func (a Agent) Signer() attribution.Signer {
	return attribution.Signer{
		Identity: record.Identity,
		Marker:   signatureMarker,
		Via:      signatureVia,
		Email:    signerEmail,
	}
}

// PreflightArgv returns goose's host one-shot argv with the prompt appended.
func (a Agent) PreflightArgv(prompt string) ([]string, bool) {
	return append(append([]string{}, record.Argv.Preflight...), prompt), true
}

// LaunchArgv builds goose's in-container argv; mirrors cmd/ward's buildAgentArgv.
// One-shot Goose reads the seed from stdin. Interactive drops it entirely.
func (a Agent) LaunchArgv(rc agentsapi.RunCtx) (argv []string, stream bool) {
	if rc.Headless || rc.Ask {
		return []string{"goose", "run", "--no-session", "-t"}, false
	}
	return []string{"goose", "session"}, false
}
