---
doc_goal: Pin the WARD_CONFIG_REF config-source seam - the fs.FS the KDL build sites compile from, the unset-means-baked selection contract, the flat bundle layout, and the per-site degrade behavior.
---
# The config source: `WARD_CONFIG_REF` and the fs.FS-at-launch seam

Since [ward#653](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/653) (epic [ward#650](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/650)), ward's KDL surfaces compile at launch from a
**selected `fs.FS`**, not a hard-wired embed. The compilers are unchanged - only
the source handle moves. `cmd/ward/configsource.go` picks it; three sites read it:

- `ward ops forgejo` - spec guardfile + swagger lock (`cmd/ward/ops.go`), plus
  the optional admin remote-exec guardfile grafted beside it.
- the exec-dialect auto-mount - `ward docker`, `ward agents <tool>`,
  `ward ops {aws,kubectl,...}` (`cmd/ward/wardkdl_exec.go`).
- the fleet roster - dialect-2 `fleetconfig.Parse` (`cmd/ward/fleet.go`).

## Selection contract

- **Unset** - the **baked neutral default**: the embedded mirrors of
  `.ward/ward-kdl/` (drift-tested). Byte-for-byte the pre-seam behavior. This
  is the whole precedence: set uses the ref, unset uses the baked default.
- **`file://<dir>`** - `os.DirFS(<dir>)` over a local bundle directory in the
  flat layout below. The local escape hatch.
- **`<host>/<owner>/<repo>[@<ref>]//<subpath>`** - the self-describing git-ref
  grammar, synced through the TTL-cached resolver
  ([config-ref-resolver.md](config-ref-resolver.md)) into a local checkout
  whose `<subpath>` dir feeds this seam. A malformed or unsyncable ref fails
  loud - an override is never silently ignored
  ([config-discovery.md](config-discovery.md) holds the same rule).

## Bundle layout

A ref points at a **flat** bundle directory - the [aos#332](https://github.com/coilysiren/agentic-os/issues/332) layout, same as `.ward/ward-kdl/`:

- `ward-kdl.forgejo.guardfile.kdl` + `forgejo.swagger.lock.json` - the spec
  surface for `ops forgejo`.
- `ward-kdl.<area>.guardfile.kdl` (exec dialect) - auto-mounted at their `wrap`
  path ([ward-kdl-in-ward.md](ward-kdl-in-ward.md)); the exec scan mounts only
  files carrying an `exec` block.
- `ward-kdl.fleet.kdl` - the dialect-2 fleet manifest.
- `ward-kdl.topology.kdl` - container topology overlay. Env wins.
- `forgejo-admin.guardfile.kdl` - **optional**; omitting it withholds
  `ops forgejo admin/doctor`.

## Failure behavior

A set-but-unresolvable ref (bad grammar, missing dir) and a bundle that fails
to parse both degrade **per site, loudly**:

- `ward ops forgejo` mounts the error leaf - the failure surfaces on
  invocation (`guardfile runtime failed to mount: ...`), so a bad bundle can
  never silently drop a verb surface.
- the exec auto-mount degrades with a stderr warning at launch.
- fleet consumers (`ward agents list`, `ward agent ...`) error at verb time.
- the rest of the CLI (`version`, `exec`, `git`, ...) keeps working; the
  `--harness` choice list is built at init from the baked roster so a bad ref
  cannot panic the binary before a verb can answer.

There is no fallback from a named-but-broken source to the baked default.

## What this replaced

The build-time overlay direction: release CI no longer overlays an aos
`ward-specs` bundle over the embeds before `go build` ([ward#644](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/644),
superseded), and no build-variant matrix exists ([release.md](release.md)).

## See also

- [config-ref-resolver.md](config-ref-resolver.md) - the git-ref resolver.
- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer the bundle comes from.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - how exec guardfiles auto-mount.
- [config-discovery.md](config-discovery.md) - the same loud-override rule for the allowlist.
