# release binaries

Each release publishes a tagged binary matrix plus checksums.

- The matrix covers the supported OS and architecture pairs.
- `SHA256SUMS` is published with the release.
- The matrix is built once on `main` as a draft release, then promoted to the
  public release tag without rebuilding.
- The release pages on Forgejo and GitHub mirror the same artifacts.
- Ward does not publish moving `release` or `latest` aliases for these binary
  artifacts; consumers install the tagged release assets directly.

## Why it exists

- the tarball or binary is what most users install.
- the checksum file is what makes the release verifiable.
- the matrix keeps the install story aligned across the supported hosts.
- draft tags and draft releases are disposable staging, not a second public
  release channel.

## User view

- pick the platform binary for your host.
- verify it with the checksum file.
- install it through the channel you prefer.

## What to expect

- the binaries are named by platform and architecture.
- the tags match the release tag on Forgejo.
- the mirror copies the same release payload, not a different build.
- Homebrew on macOS and Scoop on Windows install the matching Linux binary as
  an unexposed sidecar. `ward agent` copies that same-version binary into each
  Linux container instead of depending on its own published release at launch.

## See also

- [release.md](release.md) - the release workflow.
