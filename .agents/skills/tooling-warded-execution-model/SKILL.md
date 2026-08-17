---
name: tooling-warded-execution-model
description: Route Ward execution-model questions and changes to the owning human contract, agent procedure, repository, and container or host layer.
---

# Ward execution model

Use this skill when a request depends on where Ward behavior lives, what a
container can access, how a run reaches durable completion, or whether a
config, hook, doctrine, recovery, or rollout belongs in Ward or host
automation. Use `tooling-ward-agent` instead when the task is only to dispatch
one existing issue.

## Human dispatcher contract

Route a human to one current supported contract, not to silent implementation
states or historical issue pages.

* Product and authority boundary - [`architecture.md`](../../../docs/architecture.md).
* Roles, harnesses, and launch - [`agent.md`](../../../docs/agent.md) and
  [`agent-lifecycle.md`](../../../docs/agent-lifecycle.md).
* Run observation and recovery - [`agent-ops.md`](../../../docs/agent-ops.md)
  and [`troubleshooting.md`](../../../docs/troubleshooting.md).
* Host/container access - [`container-contract.md`](../../../docs/container-contract.md)
  and [`container.md`](../../../docs/container.md).
* Config ownership - [`config-source.md`](../../../docs/config-source.md).

If a human cannot configure, observe, depend on, maintain, or use a detail for
recovery, do not invent a human-facing contract for it. Keep immediate command
shape in help/errors and silent mechanics in code, comments, and tests.

## Agent operating procedure

Follow [`references/operating-procedure.md`](references/operating-procedure.md)
before filing or changing execution-shaped work. It is the one procedure for
layer ownership, documentation placement, source grounding, and validation.

## Fixed facts

* Every agent run uses an ephemeral container. The host checkout is not its workspace.
* The target workspace is writable. Substrate, context bundles, and repository references are read-only unless explicitly granted.
* Roles and context grant no authority.
* Issue threads and remote Git or PR evidence outlive disposable containers.
* Ward owns container behavior. Host convergence owns only the operator's
  long-lived harness outside Ward.
