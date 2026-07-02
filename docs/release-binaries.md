# Release binaries: the dual-forge matrix

Every tag publishes the **same binary matrix to both release pages** - the
canonical [Forgejo release](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/releases)
and the [GitHub mirror release](https://github.com/coilyco-flight-deck/ward/releases)
([ward#454](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/454)). The `publish-binaries` job in
[`.forgejo/workflows/release.yml`](../.forgejo/workflows/release.yml) is the one
place that builds and uploads them.

## The assets, and who installs which way

- **`ward-{darwin,linux}-{amd64,arm64}`** - four static binaries, one per
  platform. A GitHub arrival, or the container path, downloads the one for their
  machine, `chmod +x`es it, and runs it - no Go toolchain, no round-trip to
  forgejo.coilysiren.me ([ward#414](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/414), [ward#442](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/442)).
- **`SHA256SUMS`** - one digest per binary, bare basenames, so
  `sha256sum -c SHA256SUMS` verifies from whichever page the file came off.
- **Most people should still install via Homebrew** (see the
  [README](../README.md#install)): it stamps the version, adds the `warded`
  symlink, and upgrades in place. The raw binaries are for everyone the tap does
  not serve.

The formula itself stays build-from-source ([ward#116](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/116)), so `brew` never consumes
these binaries - see [release.md](release.md).

## Why the checksums cannot drift

The job builds the whole matrix **once** with `CGO_ENABLED=0` (ward is pure Go,
so every target cross-compiles from the linux/amd64 runner) and uploads the
**byte-identical files** to both forges. Same bytes in, same `SHA256SUMS` on both
pages - there is no second build to diverge ([ward#438](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/438)). The GitHub release also
reuses the exact Forgejo release body, so the notes match too.

## How the GitHub half is wired

- It is part of the **release pipeline**, not `mirror-to-github.yml` (which stays
  refs + metadata + scrub only - see [github-mirror.md](github-mirror.md)).
- It authenticates with **`GITHUB_MIRROR_PAT`**, the same PAT the mirror uses
  (scope: `repo` / contents:write on the GitHub mirror). **Provision it before a
  [ward#454](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/454) release run.** Unset, the GitHub half skips loudly and the Forgejo
  release is unaffected - matching the mirror's no-PAT convention.
- The release is authored by the PAT user, not `github-actions[bot]`, so the
  mirror's author-guarded release scrub leaves it alone ([ward#454](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/454), by design in
  [github-mirror.md](github-mirror.md)).
- The step pushes the tag to GitHub first (idempotent), creates or reuses the
  release, then replaces same-named assets so a workflow re-run is safe.

## See also

- [release.md](release.md) - the full release pipeline.
- [github-mirror.md](github-mirror.md) - the refs + metadata mirror and the scrub.
- [homebrew-build.md](homebrew-build.md) - the build-from-source formula.
