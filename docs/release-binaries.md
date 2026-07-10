# release binaries

Each release publishes a tagged binary matrix plus checksums.

- The matrix covers the supported OS and architecture pairs.
- `SHA256SUMS` is published with the release.
- The release pages on Forgejo and GitHub mirror the same artifacts.

## Why it exists

- the tarball or binary is what most users install.
- the checksum file is what makes the release verifiable.
- the matrix keeps the install story aligned across the supported hosts.

## User view

- pick the platform binary for your host.
- verify it with the checksum file.
- install it through the channel you prefer.

## What to expect

- the binaries are named by platform and architecture.
- the tags match the release tag on Forgejo.
- the mirror copies the same release payload, not a different build.

## See also

- [release.md](release.md) - the release workflow.
