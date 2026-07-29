---
doc_goal: Explain ward's three-layer split in one short page so the rest of the docs can refer back to it instead of repeating the model.
---
# ward architecture

Ward and AOS divide the runtime into four layers.

- `cli-guard` is the engine.
- specgen is the AOS build-time authoring layer for operator APIs.
- `aosguard` is the AOS operator CLI.
- `ward` is the native agent control plane.

The split is about **when** each layer runs. For terms that cross these
layers, use the canonical vocabulary in [terminology.md](terminology.md).

## What that means

- `cli-guard` owns the policy and routing framework.
- specgen turns AOS guardfiles into AOSguard's audited operator surface.
- AOSguard ships from AOS and does not depend on Ward at runtime.
- Ward ships hand-written agent, container, git, reservation, and dev-verb
  code plus typed harness mechanics and fixed workflows.

## See also

- [aosguard-boundary.md](aosguard-boundary.md) - the external operator boundary.
- [terminology.md](terminology.md) - Ward vocabulary and layer-spanning non-equivalences.
- [agent.md](agent.md) - the guarded execution layer.
- [exec-verb.md](exec-verb.md) - the repo dev-verb gate.
