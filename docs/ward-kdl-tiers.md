# ward-kdl permission tiers: layout + the ward#339 placeholders

The `ward-kdl-{read,write,admin}` binaries are the permission-tiered cut of the
spec-dialect surface: read ⊂ write ⊂ admin, composed by `inherit` (cli-guard#160),
each its own binary so a withheld verb is **absent at compile time**, not denied at
runtime. forgejo (ward#240/#278) and signoz (ward#338) are fully tiered; their
guardfiles live beside this doc's siblings under
`cmd/ward-kdl/ward-kdl-<tier>/ward-kdl.<area>.<tier>.guardfile.kdl`. The
build that compiles them is `make build-ward-kdl-tiers` (folded into
`make build-ward-kdl`), which discovers every `*.guardfile.kdl` in each tier dir
and merges those sharing a `wrap ward-kdl-<tier>` binary name. See
[ward-kdl.md](ward-kdl.md) for what ward-kdl is, and
[ward-kdl-surface.md](ward-kdl-surface.md) for the forgejo tier surface.

## The remaining areas are scaffolded as placeholders (ward#344)

The five spec-dialect areas not yet tiered - **trello, tailscale, glitchtip,
glama, skillsmp** - each have a read/write/admin **placeholder** in the tier dirs,
structured exactly like forgejo/signoz but **verbless**: the read file is the
singleton-bearing root (declares `spec`/`base-url`/`auth`, plus `restrict` where
the area gates scope - tailscale's `coily*` tailnet); write and admin `inherit`
their sibling tier by relative path. Where the tiered verbs will go, each carries:

```
// TODO(ward#339): tier verbs
```

This is structural prep for the ward#339 migration only - **no `can`/`never`/
`override` authoring, no keyword work** (that fan-out is gated on cli-guard#169).

### Why `.placeholder`, and how to wire an area in

A verbless surface cannot `lock`/`gen`: specgen prunes the spec to the referenced
operations, a no-verb file prunes to **zero paths**, and the reference-surface
build fails with `specverb: spec has no paths`. specgen's discovery is
**directory-wide** (`filepath.Glob(dir, "*.guardfile.kdl")`), so an in-glob
verbless file would red the *whole* tier build, not just itself. So each
placeholder carries a `.guardfile.kdl.placeholder` suffix that dodges the glob:
**staged in the tier dir (discoverable), excluded from the build until its verbs
land.** All five areas are staged-only; forgejo + signoz are untouched.

To wire an area in (the ward#339 per-area run):

1. `git mv` all three tier files, dropping the `.placeholder` suffix. The
   `inherit` paths are already written to the final `.guardfile.kdl` names, so the
   rename needs no path edit.
2. Replace the `TODO(ward#339): tier verbs` marker with the tier's `can`/`never`
   grants (read = get/list-shaped; write adds create/edit; admin adds delete).
3. `make build-ward-kdl` - the tier loop now discovers the file, copies the base
   `cmd/ward-kdl/<spec>` into each tier dir (gitignored, per ward#338), locks the
   pruned `*.openapi.lock.json` (tracked), and builds the binary.

The base monolith guardfiles (`cmd/ward-kdl/ward-kdl.<area>.guardfile.kdl`) and
their root specs stay canonical and untouched throughout - the tier files are
additive scaffolding beside the base, never a migration of it.

## See also

- [ward-kdl.md](ward-kdl.md) - what ward-kdl is: the build-time generator.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface.
- the wired references: forgejo
  [read](ward-kdl/ward-kdl.forgejo.read.guardfile.md) /
  [write](ward-kdl/ward-kdl.forgejo.write.guardfile.md) /
  [admin](ward-kdl/ward-kdl.forgejo.admin.guardfile.md), signoz
  [read](ward-kdl/ward-kdl.signoz.read.guardfile.md) /
  [write](ward-kdl/ward-kdl.signoz.write.guardfile.md) /
  [admin](ward-kdl/ward-kdl.signoz.admin.guardfile.md).
