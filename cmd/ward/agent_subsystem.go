package main

import (
	"fmt"
	"strings"
)

// agent_subsystem.go is the dispatch-side complement to the agentic-os doctrine
// "Front-load the context you know you need" (ward#236); see docs/agent-frontload.md.

// subsystemPointerRepo scopes the ward-specific keyword map to ward's own clone;
// any other carried repo is a silent no-op (docs/agent-frontload.md).
const subsystemPointerRepo = "coilyco-flight-deck/ward"

// subsystemPointer maps a ward subsystem to the keywords that name it in an
// issue and the in-clone docs/skills to read before the first edit.
type subsystemPointer struct {
	label    string   // human name of the subsystem, shown in the pointer block
	keywords []string // case-insensitive substrings in the title/body that trigger it
	paths    []string // repo-relative docs/skills to read first, most-canonical first
}

// agentSubsystemPointers is the static keyword -> path map (ward#236). Order is
// render order; keep keywords specific enough not to fire on a passing mention.
var agentSubsystemPointers = []subsystemPointer{
	{
		label:    "ward-kdl guardfile generator (the ward#226 unknown)",
		keywords: []string{"ward-kdl", "guardfile", "ops forgejo", "ops aws", "operator verb"},
		paths:    []string{"docs/ward-kdl.md", "docs/ward-kdl-surface.md", "docs/ward-kdl-in-ward.md"},
	},
	{
		label:    "ward exec dev-verb surface + .ward/ward.yaml",
		keywords: []string{"ward exec", "exec verb", "ward.yaml", "dev verb", "dev-verb"},
		paths:    []string{"docs/exec-verb.md"},
	},
	{
		label:    "ward agent dispatch + headless pre-flight",
		keywords: []string{"ward agent", "headless", "pre-flight", "preflight", "warded", "agent-dispatch"},
		paths:    []string{"docs/agent.md", "docs/agent-preflight.md", ".agents/skills/tooling-ward-agent/SKILL.md"},
	},
	{
		label:    "container bring-up + reaper backstop",
		keywords: []string{"reaper", "container reap", "ward-salvage", "bring-up", "bringup"},
		paths:    []string{"docs/container.md", "docs/container-reap.md"},
	},
	{
		label:    "PreToolUse hook guard",
		keywords: []string{"pretooluse", "hook guard", "path-hijack", "command -v"},
		paths:    []string{"docs/hook.md"},
	},
	{
		label:    "CI watch",
		keywords: []string{"ci watch", "ci-watch", "forgejo actions"},
		paths:    []string{"docs/ci-watch.md"},
	},
	{
		label:    "release + tap formula bump",
		keywords: []string{"release.yml", "tag-bump", "bump-tap", "tap formula", "skip-ci"},
		paths:    []string{"docs/release.md"},
	},
}

// matchSubsystemPointers returns the pointers whose keywords appear (case-
// insensitively) in the title or body, in declared order, one hit per pointer.
func matchSubsystemPointers(ref agentIssueRef, title, body string) []subsystemPointer {
	if ref.repoSlug() != subsystemPointerRepo {
		return nil
	}
	hay := strings.ToLower(title + "\n" + body)
	var hits []subsystemPointer
	for _, p := range agentSubsystemPointers {
		for _, kw := range p.keywords {
			if strings.Contains(hay, strings.ToLower(kw)) {
				hits = append(hits, p)
				break
			}
		}
	}
	return hits
}

// subsystemPointerLines renders matched pointers as flat "label - paths"
// bullets (house style: no prose tables). Empty when nothing matched.
func subsystemPointerLines(hits []subsystemPointer) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range hits {
		fmt.Fprintf(&b, "- %s - %s\n", p.label, strings.Join(p.paths, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// subsystemSeedBlock is the front-loading block appended to a matched headless
// seed (ward#236 item 1); empty when no keyword matched.
func subsystemSeedBlock(ref agentIssueRef, title, body string) string {
	lines := subsystemPointerLines(matchSubsystemPointers(ref, title, body))
	if lines == "" {
		return ""
	}
	return "Front-load before you plan: this issue names ward subsystems whose conventions, " +
		"schemas, and wiring already live in the fresh clone. Read each of these in full BEFORE your " +
		"first edit - do not defer them to lazy mid-task discovery. \"Discoverable in the clone\" is not " +
		"\"read\"; a path you only located is a gap you have not closed:\n\n" + lines + "\n\n" +
		"If the work touches a convention not listed here, find and read it the same way before editing."
}

// subsystemPreflightBlock hands the matched pointers to the pre-flight read
// (ward#236 item 2); empty when no keyword matched.
func subsystemPreflightBlock(ref agentIssueRef, title, body string) string {
	lines := subsystemPointerLines(matchSubsystemPointers(ref, title, body))
	if lines == "" {
		return ""
	}
	return "\n\nThis issue names ward subsystems whose conventions live in the clone you will get. " +
		"The detached run is expected to front-load these before its first edit:\n\n" + lines
}
