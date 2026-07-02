# ward-kdl authoring

`ward-kdl` is the build-time authoring layer: a source file in, a validated
least-privilege or fleet manifest out, embedded into `ward` with nothing fetched
at runtime. For what the layer **is**, see [ward-kdl.md](ward-kdl.md). This doc is
the one findable place for **how you author and swap a bundle** (ward#437,
ward#440).

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
authoring binary nor the tier CLIs ship on the release page (ward#455). A spec
author builds it from a ward checkout with `make build-ward-kdl` (-> `bin/ward-kdl`
plus the read/write/admin tiers). See **Bring your own specs** below to point that
build at your own deployment bundle.

## The spec bundle is a swappable build input

The KDL sources `ward-kdl` compiles are a **bundle** - the guardfiles, their spec
locks, and the fleet manifest that decide which endpoints, tokens, and owners a
build couples to. That coupling is deployment config, not engine code (ward#441):
the `base-url`, `ssm` token paths, `restrict owner` gate, and attribution
defaults all belong to whoever runs the fleet, not to `ward`. So the bundle is a
**declared, swappable build input** addressed by an **assets-dir convention**
(not a build-time variable, ward#453): the build reads it from `cmd/ward-kdl/`,
and you swap it by overlaying that directory's files.

## Bring your own specs

[examples/ward-specs/](../examples/ward-specs) is the neutral bundle a new
adopter starts from - a forgejo guardfile and a fleet manifest whose every
deployment-specific value is a placeholder. A build made from it carries no
coilyco endpoint/token/owner values, so the ward#441 finding does not reproduce
against it (proven by `TestExampleBundleHasNoCoilycoValues` in `cmd/ward`).

To build `ward` against your own deployment:

1. Copy `examples/ward-specs/*` into `cmd/ward-kdl/`, overlaying the tracked bundle.
2. Replace each placeholder (`git.example.com`, `/example/...`, `example*`,
   `example-bot`) with your deployment's values.
3. Run `make build-ward-kdl` to regenerate + re-embed the surfaces, then `make test`.

## Follow-ups

Moving ward's own (coilyco) canonical bundle up into aos - so the tracked tree
carries only the neutral example - is tracked at ward#453 (it also re-points the
brew-from-source and release build sites at the relocated bundle).

## See also

- [guardfile-grammar.md](guardfile-grammar.md) - the dialect-1 KDL grammar and a minimal guardfile.
- [ward-kdl.md](ward-kdl.md) - what the build-time authoring layer is.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface.
- [examples/ward-specs/](../examples/ward-specs) - the neutral starter bundle.
- [cmd/ward-kdl/README.md](../cmd/ward-kdl/README.md) - the bundle directory this doc describes.
