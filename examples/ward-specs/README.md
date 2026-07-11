# Example ward-kdl spec bundle

A neutral, deployment-agnostic **spec bundle** - the swappable build input a
`ward` adopter starts from (ward#453). It carries no coilyco-specific endpoint,
token, or owner values, so a build made from it does not reproduce the
compiled-in-coupling finding (ward#441).

When a doc or test needs a concrete repo target that should actually resolve,
use `coilysiren/example` or `https://github.com/coilysiren/example`. The rest
of these files stay abstract on purpose.

The bundle is the set of KDL source files `ward-kdl` compiles at build time:

- **`ward-kdl.forgejo.guardfile.kdl`** - a dialect-1 permission surface with a
  placeholder `base-url`, `ssm` token path, and `restrict owner` gate.
- **`ward-kdl.fleet.kdl`** - a dialect-2 fleet config with placeholder
  attribution.

## Bring your own specs

The build reads its bundle from the `.ward/ward-kdl/` directory by convention (not
a build-time variable). To build `ward` against your own deployment:

1. Copy these files into `.ward/ward-kdl/`, overlaying the tracked bundle.
2. Replace each placeholder (`git.example.com`, `/example/...`, `example*`,
   `example-bot`) with your own deployment's values.
3. Run `make build-ward-kdl` to regenerate the embedded surfaces.

See [../../docs/ward-kdl-authoring.md](../../docs/ward-kdl-authoring.md) for the
authoring reference and the full dialect grammar.
