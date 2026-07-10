# workspace

`make workspace` is the local development path for ward itself.

- It resolves a sibling `cli-guard` checkout through `go.work`.
- It keeps local builds off the network when the sibling checkout exists.

## When to use it

- when you are hacking ward and have a sibling cli-guard checkout.
- when you want local source changes to drive the build.
- when you do not want to depend on the Forgejo-hosted upstream during a local
  edit loop.

## What it does not do

- it does not change the release story.
- it does not change the Homebrew install story.
- it does not replace the repo gate.

## See also

- [homebrew-build.md](homebrew-build.md) - the install path.
- [release.md](release.md) - the release path.
