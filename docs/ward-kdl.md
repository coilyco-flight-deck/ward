---
doc_goal: Give one authoritative answer to "what is ward-kdl, and how is it different from ward" - the build-time authoring layer vs the run-time product - so the three-layer boundary stops being something a reader has to reconstruct from scattered hints.
---
# ward-kdl: the build-time authoring layer

**ward-kdl is the build-time authoring layer. `ward` is the run-time product
that embeds what it authors. [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard)
is the engine both stand on.** Three roles, told apart by **when** they run,
not just by what they touch. The conceptual model lives in
[architecture.md](architecture.md); this doc is the one place that pins what
`ward-kdl` itself is.

## Build time: source in, validated artifact out

You author a source file. cli-guard compiles or validates it. `ward` embeds the
result. Nothing is fetched at runtime.

That source file can be one of three dialects:

- **Dialect 1, permission surfaces** - `*.guardfile.kdl` spec + exec files.
  Least-privilege, audited, "protoc for permissions." Parsed by
  `cli/execverb` + `http/specverb`. A withheld verb is absent at compile time,
  not denied at runtime.
- **Dialect 2, fleet-config manifest** - `ward-kdl.fleet.kdl`, which names
  identity, model, endpoint, attribution, and roster defaults. Parsed by
  cli-guard `pkg/fleetconfig` and embedded via `fleetassets/`.
- **Dialect 3 boundary, operator-local** - the same `fleetconfig` parser,
  sourced from a local `~/.ward/fleet.local.kdl`, not embedded. It is tracked
  separately and does not change the embedded fleet manifest.

`ward-kdl` is `protoc` for permissions and fleet config: a source file in, a
typed least-privilege or fleet manifest out. You rarely run `ward-kdl` by
hand. You run what it produced, and you regenerate when the source changes
(`make build-ward-kdl`).

## Run time: `ward` embeds the emitted surfaces

`ward` (public face `warded`) is the product a user installs and runs. It
**embeds** the ward-kdl surfaces and exposes them as `ward ops <api>`, `ward
docker`, `ward agents <target>`, then adds the run-time-only layers ward-kdl
never produces: `ward agent` (drive a headless harness in a guarded container)
and `ward exec` (guarded dev verbs). The spec surfaces ride embedded spec locks
(`make sync-ops-assets`); the exec guardfiles auto-mount at their `wrap` path
(`make sync-exec-assets`, [ward-kdl-in-ward.md](ward-kdl-in-ward.md)). The
fleet manifest rides its own embed dir (`cmd/ward/fleetassets/`) and parser
(`cmd/ward/fleet.go` via `pkg/fleetconfig`), so config sits beside permissions
instead of inside them.

## The per-area reference docs

Every guardfile gets a generated reference doc beside it, with the reviewed copy
committed under [docs/ward-kdl/](ward-kdl/) (e.g.
[git](ward-kdl/ward-kdl.git.guardfile.md),
[forgejo](ward-kdl/ward-kdl.forgejo.guardfile.md)). Those are emitted by
ward-kdl's `surface.Markdown()`, so they describe one area's verbs and sit
**under** this doc - it is the build-time parent they all instantiate.
[ward-kdl-surface.md](ward-kdl-surface.md) is the flat index across every area.

## See also

- [cmd/ward-kdl/README.md](../cmd/ward-kdl/README.md) - the guardfiles + fleet manifest dir this doc describes (not a Go package).
- [architecture.md](architecture.md) - the three-layer model (cli-guard / ward-kdl / ward).
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface, area by area.
- [ward-kdl-tiers.md](ward-kdl-tiers.md) - the read/write/admin tier layout and the ward#344 placeholders the ward#339 fan-out wires in.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - how exec guardfiles auto-mount into `ward`.
- [FEATURES.md](FEATURES.md) - inventory of what ships today.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
