---
doc_goal: Explain ward's three-layer split in one short page so the rest of the docs can refer back to it instead of repeating the model.
---
# ward architecture

Ward and AOS divide the runtime into four layers.

- `cli-guard` is the engine.
- specgen is the AOS build-time authoring layer for operator APIs.
- `aguard` is the AOS-image operator CLI.
- `ward` is the native agent control plane.

The split is about **when** each layer runs.

## What that means

- `cli-guard` owns the policy and routing framework.
- specgen turns AOS guardfiles into Aguard's audited operator surface.
- Aguard ships with the AOS image and does not depend on Ward at runtime.
- Ward ships the hand-written agent, container, git, reservation, and dev-verb code plus its baked role and launch policy.

## See also

- [ward-kdl.md](ward-kdl.md) - the build-time layer.
- [agent.md](agent.md) - the guarded execution layer.
- [exec-verb.md](exec-verb.md) - the repo dev-verb gate.
