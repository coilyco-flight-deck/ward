---
doc_goal: Keep the release pipeline as a short user-facing reference after the docs collapse.
---
# release

Ward releases are Forgejo-canonical.

- Pushes to `main` drive the release workflow.
- The pipeline tags the release and publishes the binary matrix.
- The GitHub mirror stays a front door, not the source of truth.

## The basic shape

1. merge to `main`.
2. tag a release.
3. publish the binary matrix.
4. publish checksums.
5. update the install channel.

## Pipeline notes

- the release workflow is Forgejo-canonical.
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
