---
doc_goal: Give a reader the canonical, code-generated list of every ward agent startup role with its tagline, semantic capability preset, and invocation modes, so they can see the ward-owned embedded role defaults plus any effective fleet overlays without the page drifting from the binary.
---
# ward agent: the role roster

<!-- Generated from the code roster by `ward agent roster --markdown` (ward#348); do not edit by hand. Regenerate with `make agent-roster`. -->

A flat list of every `ward agent` startup role - the roster the binary resolves from
its embedded role defaults plus the effective fleet config's role overlays, so the page can never
drift. Each role is one entry: what the specialist does, what semantic capabilities the
preset carries, and how you invoke it (a ref acts on an issue, freeform text files or answers
it). Run `ward agent roster` (`warded roster`) for this list live at the terminal, and the
per-role docs each entry links to carry the prose detail. See
[agent.md](agent.md) for the umbrella and the `warded` public face.

- [`warded engineer`](agent-engineer.md) - Implements a ticket end to end. Capabilities: read + engineering. Modes: A ref carries that issue detached, fire-and-forget. Freeform text files an issue first, then carries it. Detached-only - interactive work funnels to the director. Role overlays: claude{model=claude-fable-5, name=opal engineer, pronouns=she, reasoning-effort=medium}; codex{model=gpt-5.4-mini, name=terran engineer, pronouns=he, reasoning-effort=medium}.
- [`warded director`](agent-director.md) - Opens the read-only director surface. Autonomous burndown is opt-in. Capabilities: read + project-management. Modes: Attached read-only control surface over a repo's backlog (`--repo` scope). Use `--burndown` or `--drain` for the autonomous heartbeat. Role overlays: claude{model=claude-opus-4-8[1m], name=fabled director, pronouns=she, reasoning-effort=high}; codex{model=gpt-5.5, name=solar director, pronouns=he, reasoning-effort=high}.
- [`warded qa`](agent-qa.md) - Inspects a candidate and posts a structured verdict comment. Capabilities: read. Modes: A ref inspects the issue, branch, pull request, and checks, then posts a structured QA verdict comment. Freeform mode is not exposed. Role overlays: claude{model=claude-opus-4-8, name=opal tester, pronouns=she, reasoning-effort=high}; codex{model=gpt-5.6-terra, name=terran tester, pronouns=he, reasoning-effort=high}.

## See also

- [agent.md](agent.md) - the `ward agent` umbrella and the `warded` public face.
- [agent-subcommands.md](agent-subcommands.md) - the roles compared, the pre-flight, the reaper backstop.
