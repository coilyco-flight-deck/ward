---
doc_goal: Explain Ward's product split in one short page so the rest of the docs can refer back to it instead of repeating the model.
---
# ward architecture

Ward's shipped product has two layers.

- `cli-guard` is the engine.
- `ward` is the native agent control plane.

The split is about **when** each layer runs. For terms that cross these
layers, use the canonical vocabulary in [terminology.md](terminology.md).

## What that means

- `cli-guard` owns the policy and routing framework.
- Ward ships hand-written agent, container, git, reservation, and dev-verb
  code plus typed harness mechanics and fixed workflows.
- Provider-specific operator tooling is external to the product.

## See also

- [enforcement-boundary.md](enforcement-boundary.md) - the executable boundary.
- [terminology.md](terminology.md) - Ward vocabulary and layer-spanning non-equivalences.
- [agent.md](agent.md) - the guarded execution layer.
- [exec-verb.md](exec-verb.md) - the repo dev-verb gate.
