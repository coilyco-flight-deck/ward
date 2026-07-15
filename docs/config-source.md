---
doc_goal: Describe the launch-time config source model in one compact place so the smaller docs tree still explains where edge surfaces come from.
---
# ward config source

Some ward surfaces resolve config at launch instead of from the repo file.

- The runtime keeps a baked default bundle.
- The baked defaults asset is mechanically derived from
  `.ward/ward-kdl/ward-kdl.defaults.kdl`.
- `WARD_CONFIG_REF` can swap in a live bundle.
- `WARD_CONFIG_REF` can also point at a local absolute or relative KDL file or
  bundle directory.
- the selected source feeds guarded edge/operator surfaces and the `ward agent`
  launch defaults when those bundle values exist.
- core agent control-plane paths fall back to the baked bundle only when the
  selected bundle does not supply a value.

## See also

- [config-discovery.md](config-discovery.md) - where repo config is found.
- [ward-kdl.md](ward-kdl.md) - the authoring layer behind the bundle.

## Launch-time sources

- baked defaults keep the binary usable out of the box.
- a live bundle lets the launch target change without rebuilding the binary.
- the selected bundle still needs to be auditable and explainable.
- the bundle's `ward.bundle.kdl` metadata names the Forgejo ops entrypoint.

When ward needs a concrete GitHub repo that actually resolves in examples or
tests, the baked bundle uses `coilysiren/example`. That repo is a public
placeholder target, not a deployment-specific dependency.

This is the seam for edge surfaces, not a place to hide repo policy. A bad or
incompatible `WARD_CONFIG_REF` can degrade the generated `ward ops ...` surface
it owns, but it must not break issue lookup, reservation, broker dispatch,
reaper comments, or container bootstrap.

For coilyco-targeted director/operator surfaces and coilyco engineer containers,
the baked neutral bundle is not good enough. If `WARD_TARGET_OWNER` or
`WARD_TARGET_REPO` names a coilyco repo and no external bundle is active, ward
fails early with a diagnostic that names the active source and the expected
`WARD_CONFIG_REF` bundle.

## The bundle ops monolith

- `guardfile.forgejo.kdl` is the self-contained compatibility monolith,
  mirroring the baked source's flattened forgejo guardfile.
- ward loads it via a byte-level parse with no file resolver, so the runtime
  ops surface must not `inherit` across files.
- the read/write/admin tier guardfiles in the bundle are role-facing (bound in
  `roles.kdl`), not the ops CLI surface.
