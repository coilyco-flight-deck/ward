---
doc_goal: Explain the Ward to Aguard boundary without implying a Ward runtime guardfile layer.
---
# ward-kdl

`ward-kdl` is a historical Ward authoring name, not a Ward runtime layer.

- The current AOS build uses specgen to turn operator guardfiles into Aguard.
- The AOS image ships both `aos` and `aguard`, installing
  `/usr/local/bin/aguard` in its final image.
- Ward does not embed or mount generated operator guardfiles at runtime.

Ward retains baked AOS-authored agent role and launch-policy data from
`.ward/ward-kdl/`. That policy supports the native `ward agent` lifecycle; it
does not create an operator command tree.

Use `aguard ops ...` for AOS-container operator work. `aos` remains the
composed-container launcher. Ward remains the governed agent and repository
development control plane.

## See also

- [ward-kdl-surface.md](ward-kdl-surface.md) - Aguard operator families.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - the no-mount boundary.
- [native policy assets](../.ward/ward-kdl/README.md) - Ward's retained inputs.
