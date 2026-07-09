---
doc_goal: Explain the Forgejo/GitHub test-workflow mirror and its drift checker so a maintainer can trust that both forges run the same CI and knows how to repair or extend the mirror without breaking the classification contract.
---
# Forgejo <-> GitHub workflow mirror

ward keeps the `test` workflow on both forges so canonical Forgejo pushes and the
GitHub mirror run the same checks. The two files are kept identical except for
`runs-on:`:

- `.github/workflows/test.yml` uses `ubuntu-latest`.
- `.forgejo/workflows/test.yml` uses `docker`.

Both jobs run inside the pinned aos dev-base container and call `go vet`,
`go test`, and `golangci-lint run ./...` directly from that image.

The GitHub file is the source of truth. The Forgejo file is its mirror.

## Linting

[`make lint-workflows`](../Makefile) runs
[`scripts/check_workflow_mirror.py`](../scripts/check_workflow_mirror.py):

- `ward exec lint-workflows` checks that the mirror pair still matches.
- `make lint-workflows ARGS=--fix` rewrites the Forgejo copy from the GitHub
  source, swapping only `runs-on:`.
- The pre-commit hook and the Go test in `scripts/` both call the same checker,
  so drift fails before landing even if one gate is skipped.

## Classification

The checker requires every workflow file in `.github/workflows/` and
`.forgejo/workflows/` to be classified.

- `test.yml` is mirrored.
- `release.yml` is Forgejo-only.

If a new workflow appears, add it to the checker before committing it.

## See also

- [release.md](release.md) - Forgejo-canonical release flow.
- [FEATURES.md](FEATURES.md) - the shipped-command inventory.
