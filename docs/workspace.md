---
doc_goal: Define Ward's local source workspace path and its boundary from release installs and repository gates.
---
# Workspace

`make workspace` is the local development path for ward itself.

- It resolves a sibling `umbra` checkout through `go.work`.
- It keeps local builds off the network when the sibling checkout exists.

## When to use it

- when you are hacking ward and have a sibling umbra checkout.
- when you want local source changes to drive the build.
- when you do not want to depend on the Forgejo-hosted upstream during a local
  edit loop.

## What it does not do

- it does not change the release story.
- it does not change the Homebrew install story.
- it does not replace the repo gate.

## See also

- [release.md](release.md) - packaged install and release path.
- [windows-development.md](windows-development.md) - native and cross-compile test lanes.
