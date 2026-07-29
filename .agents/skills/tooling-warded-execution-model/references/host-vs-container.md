# warded execution: host-ansible vs ward-container surface map

Reference for [`../SKILL.md`](../SKILL.md) - the full map behind the decision rule. The
two homes for fleet config, and how to tell which one owns a given fix.

## The two surfaces

| | Converged by **host ansible** (infrastructure repo) | Composed by **ward** at container bring-up (this repo) |
| --- | --- | --- |
| Audience | the operator's own laptop/server harness | everything an **agent inside a container** reads |
| Examples | `~/.claude/CLAUDE.md`, host hooks, host permissions; roles `agent-compose`, `claude-hooks` | `cmd/ward/container_payloads.go`, `container_bootstrap.go`, `agent_director_surface.go` |
| Lifetime | persists on a long-lived host | re-composed fresh into every throwaway container |
| Reach | does **not** reach into a container | the only thing a container reads |

## Why the container ignores host convergence

Ward writes the agent's `~/.claude/settings.json` from
`containerSettingsJSON` and its `~/.claude/CLAUDE.md` from
`containerDoctrine` (+ the mounted host context) on **each bring-up**. The
payloads live in `cmd/ward/container_payloads.go`; `composePermissions` and
`composeContext` in `cmd/ward/container_bootstrap.go` apply them. Nothing the
host's ansible converged is inherited; the container is a fresh, least-access
box that reads only what Ward composed into it.

So the surface that controls *agent-in-container* behavior is Ward's payload
and bootstrap code:

- **Permissions / hooks** an agent runs under → `containerSettingsJSON` (the
  `bypassPermissions` default + the force-push/history-rewrite deny list). This is the
  container's analogue of the host `claude-hooks` role - and it is the one that actually
  governs a warded agent.
- **Top-of-context doctrine** → `containerDoctrine` (the autonomy override, the
  `/substrate` rule, the reaper note).
- **Per-mode composition / read-only wiring** → `container_bootstrap.go` and
  `agent_director_surface.go` (`WARD_READONLY`, the read-only context block,
  the push-URL strip).

## The concrete miss this corrects

A "make the agent stop asking permission" fix was first filed against the host
`claude-hooks` ansible role (host convergence), when the real surfaces are ward's container
assets - refiled infrastructure#408 → ward#354. An agent reasoning about "where does a
config/doctrine/hook fix land" defaults to the host-ansible model because the
infrastructure repo foregrounds it, even when the system replacing that model for a
container-exclusive fleet *is* ward.

## How to check before filing

Ask: *does this change what an agent reads or is allowed to do **inside a ward
container**?*

- **Yes** → it lands in the payload or composing Go under `cmd/ward/`, in **ward**.
- **Only the operator's host harness** → the infrastructure ansible role.

When unsure, grep Ward's `container_payloads.go`, `container_bootstrap.go`, and
`agent_director_surface.go` for the surface before filing anywhere else - the
model is fully discoverable in this repo. Do not home a Ward-concept fix in
infrastructure: infrastructure is **downstream** of Ward, so authoring a
Ward-concept fix there inverts the dependency.
