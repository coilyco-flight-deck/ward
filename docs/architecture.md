---
doc_goal: Explain ward's three-layer split in one short page so the rest of the docs can refer back to it instead of repeating the model.
---
# ward architecture

`ward` is built from three layers.

- `cli-guard` is the engine.
- `ward-kdl` is the build-time authoring layer.
- `ward` is the run-time product that embeds the generated surfaces.

The split is about **when** each layer runs.

## What that means

- `cli-guard` owns the policy and routing framework.
- `ward-kdl` turns a guardfile into an audited CLI surface.
- `ward` ships the embedded surfaces plus the hand-written agent and git code.

## See also

- [ward-kdl.md](ward-kdl.md) - the build-time layer.
- [agent.md](agent.md) - the guarded execution layer.
- [exec-verb.md](exec-verb.md) - the repo dev-verb gate.
