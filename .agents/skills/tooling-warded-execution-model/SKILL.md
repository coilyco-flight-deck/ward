---
name: tooling-warded-execution-model
description: The warded-agent execution model - container lifecycle, roles, /workspace vs /substrate, and whether a fleet/hook/doctrine fix belongs in ward's container assets or a host ansible role.
---

# tooling-warded-execution-model

`ward agent` (public face `warded`) runs a **container-exclusive** fleet: every agent run
is a throwaway box, not a process on a converged host. The reflex an agent brings - "fleet
config and hooks are converged by host ansible" - is **wrong inside ward** and routes fixes
to the wrong repo. This skill is the durable corrective. Dispatch (resolving a ref, firing
a carry) is the sibling [`tooling-ward-agent`](../tooling-ward-agent/SKILL.md); this skill
is the model *under* that verb, written from ward's `docs/` and `cmd/ward/`.

## When to fire

- You are about to file or land **fleet-shaped work** - a config, a rollout, a hook, a
  permission policy, agent doctrine - and need to decide *which repo owns it*.
- You are reasoning about what a warded agent **may and may not do** (push, dispatch) or
  **why** a run salvaged, parked, or read as blocked.
- Anyone asks how the container lifecycle, `/workspace` vs `/substrate`, or the director's
  surface session work (detail in
  [`references/lifecycle-and-roles.md`](references/lifecycle-and-roles.md)).

Do **not** fire to *dispatch* an agent - that is `tooling-ward-agent`. This skill informs
the where/why; it resolves no refs.

## Host-ansible surface vs ward-container surface (the decision)

This is the miss the skill exists to fix. Fleet config has **two** homes, and the wrong
default is to assume the first:

- **Host ansible** (the **infrastructure** repo) converges the *operator's own* laptop or
  server harness - `~/.claude/CLAUDE.md`, host hooks, host permissions - via roles like
  `agent-compose` and `claude-hooks`. It persists on a long-lived host.
- **ward** composes everything an **agent inside a container** reads, fresh on every
  bring-up; nothing the host converges reaches it. The surfaces live in
  `cmd/ward/containerassets/` - `AGENTS.container.md` (doctrine),
  `settings.container.json` (the policy: `bypassPermissions` + the force-push deny),
  `entrypoint.sh` (`compose_context`/`compose_permissions`) - plus
  `cmd/ward/agent_director_surface.go` (`WARD_READONLY`).

The container **does not inherit the host's converged hooks or settings**: ward writes the
agent's `~/.claude/settings.json` and `CLAUDE.md` from those assets each bring-up. So a fix
to *how a warded agent behaves* - its permissions, its Stop/hook behavior, its doctrine -
lands in **ward's container assets**, not a host ansible role. The
concrete miss: a "stop asking permission" fix was first filed against the `claude-hooks`
ansible role, when the real surface was ward's container assets (infrastructure#408 →
ward#354). Full surface map: [`references/host-vs-container.md`](references/host-vs-container.md).

### The rule, plainly

> **Before filing host-shaped work** (config, rollout, fleet convergence, hooks, doctrine),
> determine whether the **warded-container path is the actual home first**. For a
> container-exclusive fleet it usually is. Ask: *does this change what an agent reads or is
> allowed to do **inside a ward container**?* If yes → `cmd/ward/containerassets/` (or
> composing Go), in **ward**. If it only changes the **operator's host harness** → the
> infrastructure ansible role. Infrastructure is downstream of ward, so a ward-concept fix
> homed there inverts the dependency.

## Out of scope / see also

- Lifecycle, the three roles, `/workspace` vs `/substrate`, the director's surface -
  [`references/lifecycle-and-roles.md`](references/lifecycle-and-roles.md).
- Resolving a dictated ref and firing a carry -
  [`tooling-ward-agent`](../tooling-ward-agent/SKILL.md).
- Pre-flight, reservation, credentials seeding - ward's `docs/agent-preflight.md`,
  `docs/agent-reservation.md`, `docs/agent-credentials.md`.
