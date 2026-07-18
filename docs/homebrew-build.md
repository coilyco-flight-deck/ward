# homebrew build

Ward's Homebrew formula installs the shipped binary and its `warded` symlink.
On macOS it also keeps the matching Linux binary under `libexec` for container
bootstrap. The sidecar is not exposed on `PATH`.

- The tap is Forgejo-hosted.
- The formula does not install the build-time authoring binary.
- `make` and `go` builds still happen from a source checkout.

## What the formula is for

- installing the release binary.
- giving contributors the release-era user path.
- keeping the install story simple enough for the docs front page.

## What it is not for

- building ward from source.
- authoring guardfiles.
- replacing the release pipeline.

## See also

- [release.md](release.md) - how the binary is published.
- [workspace.md](workspace.md) - local source builds.
