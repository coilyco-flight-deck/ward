# Homebrew build notes

## GOPROXY bypass

cli-guard has no semver tags yet, so consumers pin via pseudo-version.
`proxy.golang.org` 403s the fresh pseudo-version on first fetch even
though the upstream tarball is reachable. The Formula sets
`GOPROXY=direct` and `GOSUMDB=off` in the brew sandbox to bypass the
proxy for module fetches.

See coilysiren/homebrew-tap#14.
