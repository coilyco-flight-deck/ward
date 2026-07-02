# ward-kdl authoring

`ward-kdl` is the build-time authoring layer: a source file in, a validated
least-privilege or fleet manifest out, embedded into `ward` at build time with
nothing fetched at runtime. For what the layer **is**, see [ward-kdl.md](ward-kdl.md);
this doc is the one findable place for **how you author and swap a bundle**
(ward#437, ward#440).

## The dialects

- **Dialect 1 - permission surfaces.** `ward-kdl.<area>.guardfile.kdl` spec + exec
  files. Least-privilege, audited. A withheld verb is absent at compile time.
- **Dialect 2 - fleet config.** `ward-kdl.fleet.kdl`, the agent roster + launch
  shape, embedded via `cmd/ward/fleetassets/`.
- **Dialect 3 - operator-local.** `~/.ward/fleet.local.kdl`, the same parser,
  sourced locally and never embedded. See [fleet-local.md](fleet-local.md).

## The spec bundle is a swappable build input

The KDL sources `ward-kdl` compiles are a **bundle** - the guardfiles, their spec
locks, and the fleet manifest that together decide which endpoints, tokens, and
owners a build is coupled to. That coupling is deployment config, not engine code
(ward#441): the `base-url`, the `ssm` token paths, the `restrict owner` gate, and
the attribution defaults all belong to whoever runs the fleet, not to `ward`.

So the bundle is a **declared, swappable build input**, addressed by an
**assets-dir convention** (not a build-time variable, ward#453): the build reads
its bundle from the `cmd/ward-kdl/` directory, and you swap it by overlaying that
directory's files - no new hardcoded path one repo further from a cold reader.

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

The move of ward's own (coilyco) canonical bundle up into aos - so ward's tracked
tree carries only the neutral example and the deployment bundle lives beside its
siblings - is tracked as a follow-up, because it also re-points the
brew-from-source and release build sites at the relocated bundle (ward#453).

## See also

- [cmd/ward-kdl/README.md](../cmd/ward-kdl/README.md) - the bundle directory this doc describes.
- [ward-kdl.md](ward-kdl.md) - what the build-time authoring layer is.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface.
- [ward-kdl-tiers.md](ward-kdl-tiers.md) - the read/write/admin tier layout.
- [architecture.md](architecture.md) - the three-layer model.
- [examples/ward-specs/](../examples/ward-specs) - the neutral starter bundle.
