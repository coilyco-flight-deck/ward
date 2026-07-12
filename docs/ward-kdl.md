---
doc_goal: Explain ward-kdl as the build-time authoring layer and keep the guardfile question answerable in one page.
---
# ward-kdl

`ward-kdl` is the build-time authoring layer.

- It turns guardfile sources into audited CLI surfaces.
- `ward` embeds the generated output at runtime.
- Most repos do not need to author a guardfile at all.

## When you need it

- You need `ward-kdl` when you are shipping a custom operator surface.
- If you only use `ward exec` and `ward git`, `.ward/ward.yaml` is enough.

## The build-time split

- source files live in the authoring layer.
- the generator turns them into audited command trees.
- the shipped binary carries the generated tree, not the source bundle.
- ward also embeds shipped agent role presets from
  `.ward/ward-kdl/ward-kdl.role-definitions.kdl` so the role roster is
  code-generated from ward-owned product-default data, not hand-written prose
  or fleet config.

That split is why the docs still talk about `ward-kdl` even after the generated
reference pages were removed from the tree.

The per-area Markdown refs that `ward-kdl` can emit are generated output, not
committed release-era docs.

## The practical rule

If a repo contributor only wants the dev-verb gate, they do not need to learn
ward-kdl. If someone is authoring or changing a shipped operator surface, they
do.

## See also

- [ward-kdl-authoring.md](ward-kdl-authoring.md) - the authoring loop.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the generated surface.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - exec guardfile mounts.
- [ward-docker-exec.md](ward-docker-exec.md) - the shell-into-run leaf.
