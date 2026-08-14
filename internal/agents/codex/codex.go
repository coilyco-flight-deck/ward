// Package codex is the codex harness's agentsapi.Agent (ward#401 Phase 3,
// following ward#412). It owns codex's inert data record AND its capability
// behaviour: credential resolve/write and config compose live here now, not
// behind a closure into core. See docs/agent-harnesses.md.
package codex

import (
	"github.com/coilyco-flight-deck/ward/internal/agentsapi"

	"forgejo.coilysiren.me/coilyco-flight-deck/umbra/pkg/attribution"
)

const (
	signatureMarker = "<!-- ward-agent-signature -->"
	signatureVia    = "via `ward agent`"
	signerEmail     = "codex@ward.agent"
)

// record mirrors the agent-adapter manifest + the cmd/ward switches: codex is a
// scoped-context harness with no host pre-flight and a plain `codex exec` headless.
var record = agentsapi.Manifest{
	Name:            "codex",
	Binary:          "codex",
	ContextLevel:    1,
	Stream:          "none",
	Auth:            "codex-host",
	ReasoningEffort: "medium",
	Verbosity:       "low",
	Projection: agentsapi.Projection{
		InstructionSources: []string{"AGENTS.md"},
		InstructionPath:    ".codex/AGENTS.md",
		SkillsPath:         ".agents/skills",
		ConfigPaths:        []string{".codex/config.toml"},
		CredentialPaths:    []string{".codex/auth.json"},
		StatePaths:         []string{".codex"},
		OwnershipPaths:     []string{".codex", ".agents"},
	},
	Argv: agentsapi.Argv{
		Preflight:   nil,
		Headless:    []string{"codex", "exec"},
		Interactive: []string{"codex"},
	},
	Identity: attribution.Identity{Name: "Codex"},
}

// Agent is codex's agentsapi.Agent. Phase 3 (ward#425) drained the behaviour
// home, so it carries no state.
type Agent struct{}

// Compile-time proof codex implements the core contract plus its capabilities. No
// OnboardingSeeder: ComposeConfig carries the [projects] trust seed (ward#678).
var (
	_ agentsapi.Agent              = Agent{}
	_ agentsapi.CredentialProvider = Agent{}
	_ agentsapi.ConfigComposer     = Agent{}
	_ agentsapi.LaunchGate         = Agent{}
)

// New returns codex's Agent.
func New() Agent { return Agent{} }

// Name is the roster key.
func (a Agent) Name() string { return record.Name }

// Record returns codex's inert data record.
func (a Agent) Record() agentsapi.Manifest { return record }

// Signer builds codex's umbra signer; mirrors cmd/ward's agentSigner.
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
func (a Agent) LaunchArgv(rc agentsapi.RunCtx) (argv []string, stream bool) {
	if rc.Headless || rc.Ask {
		argv = []string{"codex", "exec"}
		if len(rc.Seed) > 0 {
			argv = append(argv, "--")
		}
		return append(argv, rc.Seed...), false
	}
	return append([]string{"codex"}, rc.Seed...), false
}
