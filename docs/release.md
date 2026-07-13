---
doc_goal: Keep the release pipeline as a short user-facing reference after the docs collapse.
---
# release

Ward releases are Forgejo-canonical and two-stage ([ward#1117](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1117)).

- `promote.yml` gates every `main` push (vet, test, lint) and, when green,
  fast-forwards the `release` branch to that sha with `CI_RELEASE_TOKEN`.
- `release.yml` runs on `release` pushes only, under a no-cancel concurrency
  queue: promoted shas release in sequence, never overlap-and-cancel.
- The pipeline stages the release as a draft, publishes the binary matrix, then makes the release visible.
- The GitHub mirror stays a front door, not the source of truth.

## The basic shape

1. merge to `main`.
2. promote gate goes green; `release` fast-forwards to the sha.
3. tag a draft release from `release`.
4. publish the binary matrix.
5. publish checksums.
6. publish the release.
7. update the install channel.

## Pipeline notes

- the release workflow is Forgejo-canonical.
- every step blocks behind the promote gate on `main`: a push whose vet,
  test, or lint checks fail promotes nothing, tags nothing, and publishes
  nothing. `release` never re-runs the suite - a flaky rerun must not fail
  an already-vouched promotion.
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
