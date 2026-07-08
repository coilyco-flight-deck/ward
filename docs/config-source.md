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
  flat layout below. The escape hatch, and the only form resolving today.
- **`<host>/<owner>/<repo>@<ref>//<subpath>`** - the self-describing git-ref
  grammar. **Not yet resolvable**: the TTL-cached git resolver is [ward#654](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/654)
  and will feed this same seam. Until then the form fails loud naming that
  issue - an explicit override is never silently ignored or downgraded
  ([config-discovery.md](config-discovery.md) holds the same rule).

## Bundle layout

A ref points at a **flat** bundle directory - the layout [aos#332](https://github.com/coilysiren/agentic-os/issues/332) landed in
aos's `.ward/`, identical to this repo's own `.ward/ward-kdl/`:

- `ward-kdl.forgejo.guardfile.kdl` + `forgejo.swagger.lock.json` - the spec
  surface for `ops forgejo`.
- `ward-kdl.<area>.guardfile.kdl` (exec dialect) - auto-mounted at their `wrap`
  path ([ward-kdl-in-ward.md](ward-kdl-in-ward.md)). Spec and exec files sit
  side by side, so the exec scan mounts only files carrying an `exec` block -
  the same filter `make sync-exec-assets` applies to the baked mirror.
- `ward-kdl.fleet.kdl` - the dialect-2 fleet manifest.
- `forgejo-admin.guardfile.kdl` - **optional**; omitting it withholds the
  `ops forgejo admin/doctor` slice, guardfile-style (absent, not an error).

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

- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer the bundle comes from.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - how exec guardfiles auto-mount.
- [config-discovery.md](config-discovery.md) - the same loud-override rule for the allowlist.
