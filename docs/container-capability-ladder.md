---
doc_goal: Make a reader grasp that a warded run's context scales with its driver's trust - the three WARD_CONTEXT_LEVEL rungs, why the rung follows the driver by design, and how the read-only overlay narrows what a run may do independently of what the ladder scales it to know.
---
# The progressive-capability ladder

A warded run is not one-size-fits-all: **how much context a run gets scales with how much
its driver is trusted.** That scaling is the `WARD_CONTEXT_LEVEL` ladder, composed by
`compose_context` in the [entrypoint](../cmd/ward/containerassets/entrypoint.sh) and sourced
from each agent's `contextLevel` in the fleet manifest
([agent-adapter-manifest.md](agent-adapter-manifest.md)). It is one facet of the
[container API](container-api.md). A higher rung folds more host doctrine into `~/AGENTS.md`:

- **Level 2 - full** - `AGENTS.container.md` doctrine **plus** the host cwd's `CLAUDE.md`
  **and** `AGENTS.md`. The trusted cloud harnesses (`claude`, `goose`) run here.
- **Level 1 - scoped** - doctrine plus the host cwd's `AGENTS.md` only (no `CLAUDE.md`).
  `codex` runs here.
- **Level 0 - minimal** - doctrine only, no host context. `opencode` runs here, and it
  self-installs, so it needs nothing baked into the image.

## The rung follows the driver, by design

The rung a run lands on is not a per-launch knob - it follows the `--harness`
([agent-drivers.md](agent-drivers.md)), which is the point: a less-trusted harness gets
less host doctrine by construction. The in-tree repo `AGENTS.md` still loads on top of
whatever rung applies. The level is exported into the container as `WARD_CONTEXT_LEVEL` so
the agent can see, from inside, how much ground it was given
([`AGENTS.container.md`](../cmd/ward/containerassets/AGENTS.container.md), "Context level").

## The read-only overlay is orthogonal

A read-only surface run (`WARD_READONLY=1`) is a separate narrowing on top of the rung:
the same context, but push wiring stripped and an appended overlay that turns the autonomy
doctrine into capture-and-dispatch ([agent-surface.md](agent-surface.md)). It narrows what a
run may **do**, where the ladder scales what it **knows** - the two compose independently.

## See also

- [container-api.md](container-api.md) - the API overview (mounts + file layout).
- [container-env.md](container-env.md) - the `WARD_*` env, including `WARD_CONTEXT_LEVEL`.
- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the per-driver `contextLevel` source.
- [agent-drivers.md](agent-drivers.md) - the four harnesses (`--harness`) compared.
