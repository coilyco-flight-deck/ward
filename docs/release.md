---
doc_goal: Keep the release pipeline as a short user-facing reference after the docs collapse.
---
# release

Ward releases are Forgejo-canonical.

- Pushes to `main` drive the release workflow.
- The pipeline stages the release as a draft, publishes the binary matrix, then makes the release visible.
- The GitHub mirror stays a front door, not the source of truth.

## The basic shape

1. merge to `main`.
2. tag a draft release.
3. publish the binary matrix.
4. publish checksums.
5. publish the release.
6. update the install channel.

## Pipeline notes

- the release workflow is Forgejo-canonical.
- every other step blocks behind the test gate: a `main` push whose vet, test,
  or lint checks fail tags nothing and publishes nothing.
- the published binaries should match the tagged source state.
- the install channel update should follow the release, not invent a second
  release story.

## What matters operationally

- the canonical release record lives on Forgejo.
- the GitHub mirror is useful to users but not authoritative.
- release docs should describe the shipped behavior, not the internal workflow
  step names.

## See also

- [release-binaries.md](release-binaries.md) - the published artifacts.
- [homebrew-build.md](homebrew-build.md) - the tap packaging path.
