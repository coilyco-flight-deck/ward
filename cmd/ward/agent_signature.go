package main

import (
	"fmt"
	"os"
	"strings"
)

// agent_signature.go signs the content bodies ward emits to Forgejo (and the
// reaper commit) with the driving agent's identity. See docs/agent-attribution.md.

// agentSignatureMarker is the hidden, idempotent marker on a signed body: an
// HTML comment, so it stays invisible in rendered Forgejo markdown.
const agentSignatureMarker = "<!-- ward-agent-signature -->"

// agentIdentity resolves the name and (optional) pronouns a mode signs with; an
// unrecognized mode resolves whole to the claude identity, not a half one.
func (m containerMode) agentIdentity() (name, pronouns string) {
	switch m {
	case modeCodex:
		return "Codex", ""
	case modeQwen:
		return "Qwen", ""
	case modeGoose:
		return "Goose", ""
	case modeClaude:
		return "Claude", "she/her"
	default:
		return "Claude", "she/her"
	}
}

// agentDisplayName is the human-facing name a mode signs with.
func (m containerMode) agentDisplayName() string {
	name, _ := m.agentIdentity()
	return name
}

// agentAttribution renders the one-line identity, e.g. "Claude (she/her)" when
// pronouns are known, otherwise just "Goose".
func (m containerMode) agentAttribution() string {
	name, pronouns := m.agentIdentity()
	if pronouns != "" {
		return fmt.Sprintf("%s (%s)", name, pronouns)
	}
	return name
}

// signBody idempotently appends the agent attribution footer to a markdown
// body; an empty body becomes the footer alone, never empty.
func (m containerMode) signBody(body string) string {
	if strings.Contains(body, agentSignatureMarker) {
		return body
	}
	footer := fmt.Sprintf("%s\n— %s, via `ward agent`", agentSignatureMarker, m.agentAttribution())
	if strings.TrimSpace(body) == "" {
		return footer
	}
	return strings.TrimRight(body, "\n") + "\n\n" + footer
}

// commitTrailer is the git Co-Authored-By trailer tagging a commit with the
// agent that produced the work.
func (m containerMode) commitTrailer() string {
	return fmt.Sprintf("Co-Authored-By: %s <%s@ward.agent>", m.agentAttribution(), m)
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
