# golangci

`golangci-lint` runs with the repo's strict baseline.

- The config is part of the release-era developer surface.
- Keep it aligned with the code and the pre-commit suite.

## What that means

- the lint config is a release contract.
- changes to the code should not silently loosen it.
- the docs should point to the config, not restate every rule.

## See also

- [release.md](release.md) - release checks.
- [docs/README.md](README.md) - the index of surviving docs.
