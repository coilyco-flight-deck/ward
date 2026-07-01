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

// agentIdentity resolves the name and (optional) pronouns a mode signs with; an
// unrecognized mode resolves whole to the claude identity, not a half one.
func (m containerMode) agentIdentity() (name, pronouns string) {
	switch m {
	case modeCodex:
		return "Codex", ""
	case modeOpencode:
		// The harness renamed qwen->opencode (ward#401), but the signing persona
		// stays "Qwen" - the backing model is who the work is attributed to.
		return "Qwen", ""
	case modeGoose:
		return "Goose", ""
	case modeClaude:
		return "Claude", "she/her"
	default:
		return "Claude", "she/her"
	}
}

// agentSigner builds the cli-guard signer for this mode: the mode's identity
// plus ward's idempotency marker, footer tail, and Co-Authored-By email domain.
func (m containerMode) agentSigner() attribution.Signer {
	name, pronouns := m.agentIdentity()
	return attribution.Signer{
		Identity: attribution.Identity{Name: name, Pronouns: pronouns},
		Marker:   agentSignatureMarker,
		Via:      "via `ward agent`",
		Email:    fmt.Sprintf("%s@ward.agent", m),
	}
}

// agentAttribution renders the one-line identity, e.g. "Claude (she/her)" when
// pronouns are known, otherwise just "Goose".
func (m containerMode) agentAttribution() string {
	return lookupAgent(m).Signer().Identity.Label()
}

// signBody idempotently appends the agent attribution footer to a markdown
// body; an empty body becomes the footer alone, never empty.
func (m containerMode) signBody(body string) string {
	return lookupAgent(m).Signer().SignBody(body)
}

// commitTrailer is the git Co-Authored-By trailer tagging a commit with the
// agent that produced the work.
func (m containerMode) commitTrailer() string {
	return lookupAgent(m).Signer().CommitTrailer()
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
	return modeClaude
}
