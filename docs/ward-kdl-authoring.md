---
doc_goal: Be the one findable place a spec author learns to build the non-public `ward-kdl` compiler from a clone and swap the deployment bundle - while making the reader first confirm they even need a guardfile, since most adopters run the gate on `.ward/ward.yaml` alone and never touch this build-time layer.
---
# ward-kdl authoring

`ward-kdl` is the build-time authoring layer: a source file in, a validated
least-privilege or fleet manifest out, embedded into `ward` with nothing fetched
at runtime. For what the layer **is**, see [ward-kdl.md](ward-kdl.md). This doc is
the one findable place for **how you author and swap a bundle**.

**First, confirm you need one.** Most adopters do not - the dev-verb gate is
`.ward/ward.yaml` alone. You author (or swap) a guardfile only to run your own
`ward ops` operator surface
([ward-kdl.md](ward-kdl.md#do-you-need-to-author-a-guardfile-start-here)).

## The dialects

- **Dialect 1 - permission surfaces.** `ward-kdl.<area>.guardfile.kdl` spec + exec
  files. Least-privilege, audited. A withheld verb is absent at compile time.
- **Dialect 2 - fleet config.** `ward-kdl.fleet.kdl`, the agent roster + launch
  shape, embedded via `cmd/ward/fleetassets/`.
- **Dialect 3 - operator-local.** `~/.ward/fleet.local.kdl`, the same parser,
  sourced locally and never embedded. See [fleet-local.md](fleet-local.md).

## Guardfile grammar

The dialect-1 KDL grammar (the `wrap` mount path, the spec vs exec sub-dialects
and their nodes), a minimal working guardfile, and where auth config lives are all
in [guardfile-grammar.md](guardfile-grammar.md). Start there for the syntax. The
rest of this doc is how you get the compiler and swap the bundle.

## Getting the `ward-kdl` binary

`ward-kdl` is **not** a public install artifact: the brew formula installs only
`ward`, whose embedded surfaces cover what an end user runs, so neither the
authoring binary nor the tier CLIs ship on the release page. A spec
author builds it from a ward checkout with `make build-ward-kdl` (-> `bin/ward-kdl`
plus the read/write/admin tiers). See **Bring your own specs** below to point that
build at your own deployment bundle.

## The spec bundle is a swappable build input

The KDL sources `ward-kdl` compiles are a bundle: the guardfiles, their spec
locks, and `ward-kdl.fleet.kdl`. Those values are deployment config, not engine
code, and the build swaps them through a fixed assets-dir convention.

## Bring your own specs

The canonical deployment bundle now lives in the sibling `agentic-os/ward-specs/`
checkout so `ward` can consume it as a sibling build input. [examples/ward-specs/](../examples/ward-specs)
remains the neutral starter bundle, and a build made from it carries no coilyco
endpoint/token/owner values.

To build `ward` against your own deployment:

1. Copy `examples/ward-specs/*` into `.ward/ward-kdl/`, overlaying the tracked bundle.
2. Replace each placeholder (`git.example.com`, `/example/...`, `example*`, `example-bot`) with your deployment's values.
3. Run `make build-ward-kdl`, then `make test`.

## See also

- [guardfile-grammar.md](guardfile-grammar.md) - the dialect-1 KDL grammar and a minimal guardfile.
- [ward-kdl.md](ward-kdl.md) - what the build-time authoring layer is.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface.
- [examples/ward-specs/](../examples/ward-specs) - the neutral starter bundle.
- [.ward/ward-kdl/README.md](../.ward/ward-kdl/README.md) - the bundle directory this doc describes.
