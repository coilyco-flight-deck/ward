---
doc_goal: Pin down, per harness and per context, exactly where ward's real enforcement boundary sits - the container edge plus cli-guard verb gate that is identical for every harness in the agent flow, versus the claude-only fail-open host hint - so no demo or claim mistakes the soft hint for the hard wall that backs the boundary-is-the-product thesis.
---
# Where the enforcement boundary sits, per harness

ward's claim is "it refuses Y, and proves it." The mechanism that does the
refusing differs by harness, and a demo that blurs them invites the "but that
gate doesn't cover my harness" callout. This page states, per harness, where the
boundary actually sits so the denial you show is the one that holds.

Only one harness (**claude**) has a ward-provided in-harness layer. For every
harness the hard boundary in the `ward agent` flow is the same: the container
edge plus the [cli-guard](architecture.md) verb gate at that edge.

## Two contexts, told apart

- **Host-side / contributor** - a human or agent running the harness on the host
  against a ward-managed repo. Only claude gets a ward-provided intercept.
- **Container agent flow** (`ward agent` / `warded`, any harness) - the boundary
  is the container edge plus cli-guard, identical for every harness. Inside the
  container claude's hook and `permissions.deny` are deliberately **off**
  (`bypassPermissions`, no deny wall - see
  [container-permissions.md](container-permissions.md)), because the container's
  isolation plus doctrine are the real boundary. So even for claude the
  container-flow boundary is the container, not the hook.

## Per harness

- **claude** - host-side: the `ward hook pre-tool-use` PreToolUse hook (a
  best-effort routing **hint** that **fails open** - see [hook.md](hook.md)) plus
  claude's own `permissions.deny` for hard denial. Container flow: container edge
  + cli-guard, hook and deny off.
- **codex** - host-side: none ward-provided (in-container it runs
  `sandbox_mode = danger-full-access`, `approval_policy = never`). Container
  flow: container edge + cli-guard.
- **goose** - host-side: none ward-provided. Container flow: container edge +
  cli-guard.
- **opencode** - host-side: none ward-provided. Container flow: container edge +
  cli-guard.

The PreToolUse hook is **claude-only**. It does not exist for codex, goose, or
opencode, and even for claude it is a soft host-side hint that is off inside the
container. Do not let a demo imply it guards the others.

## What this means for a demo

The launch thesis is "the boundary is the product." A clip of the PreToolUse
hook declining a command is a claude-only, fail-open **hint**, not the hard gate.
The hard denial the thesis rests on is the compiled cli-guard verb gate (the
[README](../README.md) "The gate says no" demo) and the container edge. Name the
harness when you show a denial, so the boundary on screen is the one that
actually holds.

## See also

- [comparison-openshell.md](comparison-openshell.md) - "The boundary is the product" and the verb-gate scope note.
- [hook.md](hook.md) - the claude PreToolUse hook (fail-open by design).
- [container-permissions.md](container-permissions.md) - why the container flow runs with the deny wall off.
- [agent.md](agent.md) - the `ward agent` umbrella and `--driver` roster.
- [agent-drivers.md](agent-drivers.md) - the four `--driver` harnesses compared (credentials, install, launch gates).
