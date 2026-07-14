---
doc_goal: Codify the warded kernel boundary so later extraction work can say what stays inside the kernel and what belongs to the edge surfaces.
---
# warded kernel boundary

The warded workflow kernel is the shared control plane that must stay coherent
across the agent lifecycle. Extraction work should use this page as the
boundary contract.

## Kernel

These belong to the kernel when a change alters their policy, data flow, or
visible semantics:

- dispatch broker routing for `ward agent` and `warded`.
- reservation hold, release, stale-hold cleanup, and conflict logic.
- repo-working state, including issue ownership and working-cap checks.
- issue and PR workflow state machine, including `WARDED_WORKFLOW` comment
  semantics.
- hidden container bring-up, teardown, drain, and reaper handoff.
- log surfaces that explain the run after launch.
- typed tracker and harness contracts used to drive the run.
- workflow comment parsing, synthesis, and mirroring.

## Edge surfaces

These are edge surfaces unless they change the kernel contract above:

- generated ops leaves and other provider-specific command surfaces.
- launch-time config bundles and KDL authoring surfaces.
- forge and tracker adapters, plus transport glue.
- public docs that describe the kernel but do not change it.

## How to classify extraction work

- If the change decides whether a run may launch, reserve, drain, or merge, it
  is kernel work.
- If the change only adapts a provider API or exposes a command around the
  kernel, it is edge work.
- If a change crosses the seam, the issue should name the kernel half and the
  edge half separately.

## See also

- [agent.md](agent.md)
- [agent-lifecycle.md](agent-lifecycle.md)
- [agent-workflow.md](agent-workflow.md)
- [agent-director.md](agent-director.md)
- [agent-dispatch-broker.md](agent-dispatch-broker.md)
- [agent-ops.md](agent-ops.md)
- [container-lifecycle.md](container-lifecycle.md)
