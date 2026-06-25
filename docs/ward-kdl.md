---
doc_goal: Give one authoritative answer to "what is ward-kdl, and how is it different from ward" - the build-time generator vs the run-time product - so the three-layer boundary stops being something a reader has to reconstruct from scattered hints.
---
# ward-kdl: the build-time generator

**ward-kdl is the build-time generator that emits an audited CLI. `ward` is the
run-time product that embeds what it emits. [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard)
is the engine both stand on.** Three roles, told apart by **when** they run, not
just by what they touch. The conceptual model lives in
[architecture.md](architecture.md); this doc is the one place that pins what
`ward-kdl` itself is.

## Build time: a guardfile in, a least-privilege CLI out

You write a **guardfile** - a small [KDL](https://kdl.dev) policy file naming an
API's allowed verbs (`can get "*"`, `never delete "*"`). `ward-kdl` is the
cli-guard `specverb-gen` driver wrapped around that guardfile: it compiles the
guardfile plus the API's OpenAPI/Swagger spec into a **scoped, audited CLI
binary** whose every leaf is a verb you granted and whose every call audit-logs
through ward.

It is **`protoc` for permissions**: a grammar (the guardfile) in, a typed
least-privilege surface out. A withheld verb is **absent at compile time**, not
denied at runtime - `ward-kdl-read` has no `delete` leaf to reach. You rarely run
`ward-kdl` by hand. You run what it produced, and you regenerate when the
guardfile changes (`make build-ward-kdl`).

Two dialects come out of it:

- **spec dialect** (`specverb`) - REST verbs against an OpenAPI/Swagger spec:
  forgejo, trello, tailscale, glitchtip, signoz, glama, skillsmp.
- **exec dialect** (`execverb`) - allowlisted passthroughs over a local CLI:
  docker, aws, kubectl, the agent launchers, git, brew.

## Run time: `ward` embeds the emitted surfaces

`ward` (public face `warded`) is the product a user installs and runs. It
**embeds** the ward-kdl-generated surfaces and exposes them as `ward ops <api>`,
`ward docker`, `ward agents <target>`, then adds the run-time-only layers
ward-kdl never produces: `ward agent` (drive a headless harness in a guarded
container) and `ward exec` (guarded dev verbs). The spec surfaces ride embedded
spec locks (`make sync-ops-assets`); the exec guardfiles auto-mount at their
`wrap` path (`make sync-exec-assets`, [ward-kdl-in-ward.md](ward-kdl-in-ward.md)).

## The per-area reference docs

Every guardfile gets a generated reference doc beside it, with the reviewed copy
committed under [docs/ward-kdl/](ward-kdl/) (e.g.
[git](ward-kdl/ward-kdl.git.guardfile.md),
[forgejo](ward-kdl/ward-kdl.forgejo.guardfile.md)). Those are emitted by
ward-kdl's `surface.Markdown()`, so they describe one area's verbs and sit
**under** this doc - it is the build-time parent they all instantiate.
[ward-kdl-surface.md](ward-kdl-surface.md) is the flat index across every area.

## See also

- [cmd/ward-kdl/README.md](../cmd/ward-kdl/README.md) - the guardfiles+locks dir this doc describes (not a Go package).
- [architecture.md](architecture.md) - the three-layer model (cli-guard / ward-kdl / ward).
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface, area by area.
- [ward-kdl-tiers.md](ward-kdl-tiers.md) - the read/write/admin tier layout and the ward#344 placeholders the ward#339 fan-out wires in.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - how exec guardfiles auto-mount into `ward`.
- [FEATURES.md](FEATURES.md) - inventory of what ships today.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
