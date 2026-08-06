---
doc_goal: Define the complete maintainer contract for promotion, published binaries, checksums, and install channels.
---
# Ward release contract

Forgejo is canonical. Every `main` push enters a two-stage pipeline.

## Promotion

`promote.yml` runs vet, tests, and lint, builds the binary matrix once,
publishes commit-scoped draft assets with checksums, refreshes the
`ward:release` container alias, then fast-forwards the `release` branch. A
failed gate promotes nothing.

## Publication

`release.yml` consumes the exact promoted SHA under a no-cancel queue. It
retags the already-built draft assets to the public version, verifies each
staged byte against `SHA256SUMS`, and uploads the stable assets. The uploaded
assets are read back through the same resolver again before publication.

Published assets cover the supported Darwin, Linux, and Windows architecture
matrix plus `SHA256SUMS`. Binary assets use immutable version tags. The GitHub
mirror receives the same verified files and remains a contributor front door,
not the canonical release record.

Ward does not publish moving `release` or `latest` aliases.

## Install channels

* Homebrew installs `ward`, the `warded` symlink, and on macOS the matching
  Linux binary used for container bootstrap.
* Scoop installs the Windows binary and matching Linux binary.
* Install channels consume tagged release assets directly.
* For channel updates, the Scoop bucket bump is best-effort. Its failure does
  not invalidate published assets.

The package supplies its same-version Linux bootstrap binary. Explicit version
pins may fetch immutable release assets. Release notes summarize the promoted
compare range and collapse routine internal churn.

## Maintainer checks

* Change workflow mirrors through the repo's mirror generator and lint command.
* Keep the release contract test aligned with repo-local helper calls and job dependencies.
* Never rebuild promoted binaries in stage two.
* Never publish before checksum and read-back verification succeeds.
* Treat draft tags and releases as disposable transport, not public channels.

## See also

* [workspace.md](workspace.md) - source builds.
* [compat-surface.md](compat-surface.md) - supported host and provider surfaces.
