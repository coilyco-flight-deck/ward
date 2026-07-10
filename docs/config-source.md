---
doc_goal: Describe the launch-time config source model in one compact place so the smaller docs tree still explains where edge surfaces come from.
---
# ward config source

Some ward surfaces resolve config at launch instead of from the repo file.

- The runtime keeps a baked default bundle.
- `WARD_CONFIG_REF` can swap in a live bundle.
- the selected source feeds guarded edge surfaces and agent defaults.

## See also

- [config-discovery.md](config-discovery.md) - where repo config is found.
- [ward-kdl.md](ward-kdl.md) - the authoring layer behind the bundle.

## Launch-time sources

- baked defaults keep the binary usable out of the box.
- a live bundle lets the launch target change without rebuilding the binary.
- the selected bundle still needs to be auditable and explainable.

This is the seam for edge surfaces, not a place to hide repo policy.
