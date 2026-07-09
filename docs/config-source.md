---
doc_goal: Pin the WARD_CONFIG_REF edge-surface config-source seam - the fs.FS the guarded KDL build sites compile from, the unset-means-baked selection contract, the flat bundle layout, and the per-site degrade behavior.
---
# The edge config source: `WARD_CONFIG_REF` and the fs.FS-at-launch seam

Since [ward#653](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/653) (epic [ward#650](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/650)), ward's edge-mounted KDL surfaces compile at launch from a selected `fs.FS`, not a hard-wired embed. `cmd/ward/configsource.go` picks it. The baked agent/container defaults stay ward-owned, and the launch-selected smart-defaults bundle reads the same selection path for runtime policy. Three sites read it:

- `ward ops forgejo` - spec guardfile + swagger lock, plus the optional admin guardfile.
- the exec-dialect auto-mount - `ward docker`, `ward agents <tool>`, `ward ops {aws,kubectl,...}`.

## Selection contract

- **Unset** - the baked neutral default: the embedded mirrors of `.ward/ward-kdl/` (drift-tested).
- **`file://<dir>`** - `os.DirFS(<dir>)` over a local bundle directory in the
  flat layout below.
- **`<host>/<owner>/<repo>[@<ref>]//<subpath>`** - the git-ref grammar, synced through the TTL-cached resolver ([config-ref-resolver.md](config-ref-resolver.md)).

## Bundle layout

A ref points at a **flat** bundle directory - the [aos#332](https://github.com/coilysiren/agentic-os/issues/332) layout, same as `.ward/ward-kdl/`:

- `ward-kdl.forgejo.guardfile.kdl` + `forgejo.swagger.lock.json` - the spec
  surface for `ops forgejo`.
- `ward-kdl.<area>.guardfile.kdl` (exec dialect) - auto-mounted at their `wrap`
  path ([ward-kdl-in-ward.md](ward-kdl-in-ward.md)); the exec scan mounts only
  files carrying an `exec` block.
- `ward-kdl.fleet.kdl` - the dialect-2 fleet manifest.
- `ward-kdl.defaults.kdl` - the launch-selected smart defaults for runtime policy knobs like reservation TTL, director cadence, container retention, and the repo-authority policy block that drives trusted-owner checks and GitHub-vs-Forgejo resolution.
- `ward-kdl.topology.kdl` - container topology overlay. Env wins.
- `forgejo-admin.guardfile.kdl` - **optional**; omitting it withholds
  `ops forgejo admin/doctor`.

## Failure behavior

A set-but-unresolvable ref and a bundle that fails to parse both degrade **per site, loudly**:

- `ward ops forgejo` mounts the error leaf - the failure surfaces on
  invocation (`guardfile runtime failed to mount: ...`), so a bad bundle can
  never silently drop a verb surface.
- the exec auto-mount degrades with a stderr warning at launch.
- the edge ops bundle consumers (`ward ops forgejo`, `ward docker`, `ward ops
  aws`, `ward ops kubectl`, `ward agents <tool>`) error at verb time.
- core runtime consumers (`ward agent`, container bring-up, reservation
  defaults, review timing) keep their ward-owned defaults and ignore this seam.
- the rest of the CLI (`version`, `exec`, `git`, ...) keeps working.

There is no fallback from a named-but-broken edge source to the baked default.

## See also

- [config-ref-resolver.md](config-ref-resolver.md) - the git-ref resolver.
- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer the bundle comes from.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - how exec guardfiles auto-mount.
- [config-discovery.md](config-discovery.md) - the same loud-override rule for the allowlist.
