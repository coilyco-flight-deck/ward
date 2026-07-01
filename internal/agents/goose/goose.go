// Package goose is the goose harness's agentspi.Agent (ward#412, Phase 2 of
// ward#401). It carries goose's inert data record and forwards its capability
// behaviour to the still-live cmd/ward funcs via closures core injects; no
// behaviour lives here. See docs/agentspi.md.
package goose

import (
	"github.com/coilyco-flight-deck/ward/internal/agentspi"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"
)

const (
	signatureMarker = "<!-- ward-agent-signature -->"
	signatureVia    = "via `ward agent`"
	signerEmail     = "goose@ward.agent"
)

// record mirrors the agent-adapter manifest + the cmd/ward switches; goose's
// ollama endpoint is composed into config, so it is a ConfigComposer only.
var record = agentspi.Manifest{
	Name:         "goose",
	Binary:       "goose",
	ContextLevel: 2,
	Stream:       "none",
	Auth:         "ollama",
	Argv: agentspi.Argv{
		Preflight:   []string{"goose", "run", "-t"},
		Headless:    []string{"goose", "run", "-t"},
		Interactive: []string{"goose", "session"},
	},
	Identity: attribution.Identity{Name: "Goose"},
}

// Agent is goose's agentspi.Agent; core injects the config-composer closure
// (composeGooseConfig, which also seeds the host-resolved ollama endpoint).
type Agent struct {
	ComposeConfigFn func(agentspi.RunCtx) error // -> composeGooseConfig
}

// Compile-time proof goose implements the core contract plus its one capability.
var (
	_ agentspi.Agent          = Agent{}
	_ agentspi.ConfigComposer = Agent{}
)

// New returns goose's Agent with no capabilities wired (DATA-only).
func New() Agent { return Agent{} }

// Name is the roster key.
func (a Agent) Name() string { return record.Name }

// Record returns goose's inert data record.
func (a Agent) Record() agentspi.Manifest { return record }

// Signer builds goose's cli-guard signer; mirrors cmd/ward's agentSigner.
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
// Interactive drops the seed (a goose session is not auto-fed the prompt).
func (a Agent) LaunchArgv(rc agentspi.RunCtx) (argv []string, stream bool) {
	if rc.Headless || rc.Ask {
		return append([]string{"goose", "run", "-t"}, rc.Seed...), false
	}
	return []string{"goose", "session"}, false
}

// ComposeConfig writes goose's provider/model config (+ host ollama endpoint).
func (a Agent) ComposeConfig(rc agentspi.RunCtx) error {
	if a.ComposeConfigFn == nil {
		return nil
	}
	return a.ComposeConfigFn(rc)
}
