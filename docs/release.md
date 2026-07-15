---
doc_goal: Keep the release pipeline as a short user-facing reference after the docs collapse.
---
# release

Ward releases are Forgejo-canonical and two-stage ([ward#1117](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1117)).

- `promote.yml` gates every `main` push (vet, test, lint), builds the binary
  matrix once, publishes it as a commit-scoped draft release, and only then
  fast-forwards the `release` branch with `CI_RELEASE_TOKEN`.
- `release.yml` consumes the promoted `release` sha, retags the already-built
  draft assets to the public version, and updates install channels under a
  no-cancel concurrency queue: promoted shas release in sequence, never
  overlap-and-cancel.
- `promote.yml` also refreshes the `ward:release` container image alias before
  promotion with a registry copy helper, and `release.yml` verifies that alias
  resolves before it publishes.
- The pipeline stages the release on `main` and promotes the prebuilt assets on
  `release`, rather than rebuilding them twice.
- The GitHub mirror stays a front door, not the source of truth.

## The basic shape

1. merge to `main`.
2. promote gate goes green; `main` builds a draft release for that sha.
3. the draft publish succeeds.
4. `release` fast-forwards to the sha.
5. release workflow consumes that promoted sha.
6. retag the draft assets to the public version.
7. publish the release.
8. update the install channel.

## Pipeline notes

- the release workflow is Forgejo-canonical.
- every step blocks behind the promote gate on `main`: a push whose vet, test,
  or lint checks fail promotes nothing, and the release push only publishes the
  already-vouched sha.
- the tag and release creation steps run from repo-local helpers, so a transient
  fetch timeout in the shared actions repo cannot block release setup.
- the published binaries are built once on `main` and then retagged on
  `release`.
- Ward publishes immutable version tags and checksums, not moving `release` or
  `latest` aliases.
- the Scoop bucket bump is best-effort: if the write token is absent or the
  bucket push fails, the Forgejo release still stays green and the manifest can
  be retried separately.
- the Scoop manifest job reads the `.sha256` sidecar asset body from the
  release API, not the asset metadata JSON.
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
