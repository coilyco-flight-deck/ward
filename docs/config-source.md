---
doc_goal: Describe the launch-time config source model in one compact place so the smaller docs tree still explains where edge surfaces come from.
---
# ward config source

Some ward surfaces resolve config at launch instead of from the repo file.

* The runtime keeps a baked default bundle.
* The baked defaults asset is mechanically derived from
  `.ward/ward-kdl/ward-kdl.defaults.kdl`.
* `config-ref` in `~/.ward/config.yaml` sets the durable operator-local bundle.
* `WARD_CONFIG_REF` overrides that value for one launch.
* Either setting can point at a local absolute or relative KDL file or
  bundle directory, including the file path printed by `ward setup`.
* The selected source feeds guarded edge/operator surfaces and the `ward agent`
  launch defaults when those bundle values exist.
* Core agent control-plane paths fall back to the baked bundle only when the
  selected bundle does not supply a value.

## See also

- [config-discovery.md](config-discovery.md) - where repo config is found.
- [ward-kdl.md](ward-kdl.md) - the authoring layer behind the bundle.

## Launch-time sources

Ward resolves sources in this order:

1. `WARD_CONFIG_REF`
2. `config-ref` in `~/.ward/config.yaml`
3. config reconstructed from coilyco target metadata
4. baked neutral defaults

`ward setup`, `ward doctor`, and the generated operator surface report which
source won. A malformed or unavailable configured source fails loudly.

* Baked defaults keep the binary usable out of the box.
* A live bundle lets the launch target change without rebuilding the binary.
* The selected bundle still needs to be auditable and explainable.
* The bundle's `ward.bundle.kdl` metadata names the Forgejo ops entrypoint.

When ward needs a concrete GitHub repo that actually resolves in examples or
tests, the baked bundle uses `coilysiren/example`. That repo is a public
placeholder target, not a deployment-specific dependency.

This is the seam for edge surfaces, not a place to hide repo policy. A bad or
incompatible selected config ref can degrade the generated `ward ops ...` surface
it owns, but it must not break issue lookup, reservation, broker dispatch,
reaper comments, or container bootstrap.

For coilyco-targeted director/operator surfaces and coilyco engineer containers,
the baked neutral bundle is not good enough. If `WARD_TARGET_OWNER` or
`WARD_TARGET_REPO` names a coilyco repo and no external bundle is active, ward
fails early with a diagnostic that names the active source and the expected
`WARD_CONFIG_REF` or operator-local `config-ref` bundle.

## The bundle ops monolith

- `guardfile.forgejo.kdl` is the self-contained compatibility monolith,
  mirroring the baked source's flattened forgejo guardfile.
- ward loads it via a byte-level parse with no file resolver, so the runtime
  ops surface must not `inherit` across files.
- the read/write/admin tier guardfiles in the bundle are role-facing (bound in
  `roles.kdl`), not the ops CLI surface.
