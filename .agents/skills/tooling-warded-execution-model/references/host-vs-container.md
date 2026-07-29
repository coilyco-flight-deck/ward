# warded execution: host-ansible vs ward-container surface map

Reference for [`../SKILL.md`](../SKILL.md) - the full map behind the decision rule. The
two homes for fleet config, and how to tell which one owns a given fix.

## The two surfaces

| | Converged by **host ansible** (infrastructure repo) | Composed by **ward** at container bring-up (this repo) |
| --- | --- | --- |
| Audience | the operator's own laptop/server harness | everything an **agent inside a container** reads |
| Examples | `~/.claude/CLAUDE.md`, host hooks, host permissions; roles `agent-compose`, `claude-hooks` | `cmd/ward/containerassets/AGENTS.container.md`, `entrypoint.sh`, `cmd/ward/container_bootstrap.go`, `cmd/ward/agent_director_surface.go` |
| Lifetime | persists on a long-lived host | re-composed fresh into every throwaway container |
| Reach | does **not** reach into a container | the only thing a container reads |

## Why the container ignores host convergence

ward writes the agent's `~/.claude/settings.json` from typed Go policy and composes its
context from `AGENTS.container.md` (+ the mounted host context) on **each bring-up** -
the `composePermissions` and `composeContext` functions in
`cmd/ward/container_bootstrap.go`. Nothing the host's ansible converged is inherited; the
container is a fresh, least-access box that reads only what ward composed into it.

So the surfaces that control *agent-in-container* behavior are ward's bootstrap and
container assets:

- **Permissions / hooks** an agent runs under → typed policy in
  `container_bootstrap.go` (including the `bypassPermissions` default). This is the
  container's analogue of the host `claude-hooks` role - and it is the one that actually
  governs a warded agent.
- **Top-of-context doctrine** → `AGENTS.container.md` (the autonomy override, the
  `/substrate` rule, the reaper note).
- **Per-mode composition / read-only wiring** → `container_bootstrap.go` and
  `agent_director_surface.go` (`WARD_READONLY`, the read-only context block, the
  push-URL strip).

## The concrete miss this corrects

A "make the agent stop asking permission" fix was first filed against the host
`claude-hooks` ansible role (host convergence), when the real surfaces are ward's container
bootstrap and assets - refiled infrastructure#408 → ward#354. An agent reasoning about "where does a
config/doctrine/hook fix land" defaults to the host-ansible model because the
infrastructure repo foregrounds it, even when the system replacing that model for a
container-exclusive fleet *is* ward.

## How to check before filing

Ask: *does this change what an agent reads or is allowed to do **inside a ward
container**?*

- **Yes** → it lands in `cmd/ward/container_bootstrap.go` or
  `cmd/ward/containerassets/`, in **ward**.
- **Only the operator's host harness** → the infrastructure ansible role.

When unsure, grep ward's `cmd/ward/container_bootstrap.go` and
`cmd/ward/containerassets/` for the surface before filing anywhere else - the model is
fully discoverable in this repo. Do not home a ward-concept fix in infrastructure:
infrastructure is **downstream** of ward, so authoring a ward-concept fix there inverts
the dependency.
