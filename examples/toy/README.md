# toy - ward's example repo

The minimal, self-contained repo that shows what a ward-managed project looks
like. It is the anchor the demo (ward#250 / #251) and the minimal example spec
bundle (ward#453) point at, and it was formerly sketched as "seed" (ward#463).

Everything here is deliberately tiny and dependency-free (a pure POSIX-`sh`
`greet` CLI) so the verb gate runs anywhere with no toolchain to install.

## Layout

- `greet.sh` - the toy CLI. `sh greet.sh <name>` prints `hello, <name>`.
- `test.sh` - one assertion, non-zero on mismatch.
- `Makefile` - `build` / `test` / `vet` / `install`, each with a `## help`
  comment. The former `ward setup` / `ward doctor` shape is captured in the
  release-planning inventory.
- `.ward/ward.yaml` - the allowlist: the build/test/install triple plus a
  `security:` block (required per ward#450).
- `toy.guardfile.kdl` - a minimal ward-kdl permission surface (deny-by-default).

## Try it

```sh
cd examples/toy
ward exec build     # runs `make build` through the gate, one audit row
ward exec test      # runs `make test`
```

For the launch demo - one happy path plus three danger classes, driven against
this repo - run `sh ../demo.sh` from here (or `sh examples/demo.sh` from the
repo root). Walkthrough: [../../docs/demo.md](../../docs/demo.md).

## The dev-base image

A `ward agent` run against a repo like this pulls the aos-published **dev-base**
container image and clones fresh inside it - see
[../../docs/container-image.md](../../docs/container-image.md). Nothing here
bakes that image; the toy is what the agent clones, not what it runs in.

Full walkthrough: [../../docs/example-repo.md](../../docs/example-repo.md).
