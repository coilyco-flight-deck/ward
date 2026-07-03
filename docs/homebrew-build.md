---
doc_goal: Let a maintainer touching the brew formula understand why it forces GOPROXY=direct and GOSUMDB=off - the untagged cli-guard pseudo-version that proxy.golang.org 403s - so the workaround is not stripped out and a from-source install keeps working.
---
# Homebrew build notes

## GOPROXY bypass

cli-guard has no semver tags yet, so consumers pin via pseudo-version.
`proxy.golang.org` 403s the fresh pseudo-version on first fetch even
though the upstream tarball is reachable. The Formula sets
`GOPROXY=direct` and `GOSUMDB=off` in the brew sandbox to bypass the
proxy for module fetches.

## When this can be removed

The override is temporary and keyed to a single condition: cli-guard has no
semver tags yet. Once cli-guard ships tagged releases, `proxy.golang.org`
serves the module normally and the fetch no longer 403s, so the
`GOPROXY=direct` and `GOSUMDB=off` lines in the Formula's brew-sandbox block
can drop. A maintainer touching the Formula should check for a cli-guard tag
before assuming the workaround is still load-bearing - it is safe to revisit
the moment that tag exists, not before.

See [coilysiren/homebrew-tap#14](https://github.com/coilysiren/homebrew-tap/issues/14).
