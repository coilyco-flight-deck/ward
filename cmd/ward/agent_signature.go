package main

import (
	"fmt"
	"os"
	"strings"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/attribution"
)

// agent_signature.go signs ward's Forgejo bodies and reaper commit with the
// agent's identity via cli-guard's pkg/attribution. See docs/agent-attribution.md.

// agentSignatureMarker is the hidden, idempotent marker on a signed body: an
// HTML comment, so it stays invisible in rendered Forgejo markdown.
const agentSignatureMarker = "<!-- ward-agent-signature -->"

const (
	envAgentDisplayName = "WARD_AGENT_DISPLAY_NAME"
	envAgentPronouns    = "WARD_AGENT_PRONOUNS"
)

// defaultAgentIdentity resolves the harness default name and pronouns; an
// unrecognized mode resolves whole to the claude identity, not a half one.
func (m containerMode) defaultAgentIdentity() attribution.Identity {
	switch m {
	case modeCodex:
		return attribution.Identity{Name: "Codex"}
	case modeOpencode:
		// The harness renamed qwen->opencode (ward#401), but the signing persona
		// stays "Qwen" - the backing model is who the work is attributed to.
		return attribution.Identity{Name: "Qwen"}
	case modeGoose:
		return attribution.Identity{Name: "Goose"}
	case modeClaude:
		return attribution.Identity{Name: "Claude", Pronouns: "she/her"}
	default:
		return attribution.Identity{Name: "Claude", Pronouns: "she/her"}
	}
}

func agentIdentityFromEnv(mode, name, pronouns string) attribution.Identity {
	identity := containerMode(mode).defaultAgentIdentity()
	if v := strings.TrimSpace(name); v != "" {
		identity.Name = v
	}
	if v := strings.TrimSpace(pronouns); v != "" {
		identity.Pronouns = v
	}
	return identity
}

// agentSigner builds the cli-guard signer for this mode: the mode's identity
// plus ward's idempotency marker, footer tail, and Co-Authored-By email domain.
func (m containerMode) agentSigner() attribution.Signer {
	identity := agentIdentityFromEnv(string(m), os.Getenv(envAgentDisplayName), os.Getenv(envAgentPronouns))
	return attribution.Signer{
		Identity: identity,
		Marker:   agentSignatureMarker,
		Via:      "via `ward agent`",
		Email:    fmt.Sprintf("%s@ward.agent", m),
	}
}

// agentAttribution renders the one-line identity, e.g. "Claude (she/her)" when
// pronouns are known, otherwise just "Goose".
func (m containerMode) agentAttribution() string {
	return m.agentSigner().Identity.Label()
}

// signBody idempotently appends the agent attribution footer to a markdown
// body; an empty body becomes the footer alone, never empty.
func (m containerMode) signBody(body string) string {
	return m.agentSigner().SignBody(body)
}

// commitTrailer is the git Co-Authored-By trailer tagging a commit with the
// agent that produced the work.
func (m containerMode) commitTrailer() string {
	return m.agentSigner().CommitTrailer()
}

// currentAgentMode resolves the running context's agent from WARD_AGENT then
// WARD_MODE, defaulting to claude when unset or unrecognized.
func currentAgentMode() containerMode {
	v := strings.TrimSpace(os.Getenv("WARD_AGENT"))
	if v == "" {
		v = strings.TrimSpace(os.Getenv("WARD_MODE"))
	}
	if m, err := parseMode(v); err == nil {
		return m
	}
	return defaultAgentMode()
}
