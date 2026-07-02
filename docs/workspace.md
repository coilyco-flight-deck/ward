# Working on cli-guard side by side (Go workspace)

ward consumes [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) as a separate Go module, pinned in `go.mod` (and via the Makefile `REF` for the `specverb-gen` driver). Without a workspace, every cli-guard change you want to use here costs a full cross-module release: tag + release cli-guard, `go get` the new version into ward's `go.mod`, bump the Makefile `REF`, then the change is usable.

A Go workspace collapses that loop for local dev. With cli-guard checked out beside ward:

```
ward/         <- you are here
cli-guard/    <- ../cli-guard
```

run:

```
make workspace
```

That writes a **gitignored** `go.work` (`use (. ../cli-guard)`). cli-guard now resolves from your local working tree for `go build`, `ward exec`, and `specverb-gen` runs - **no tag, no `go get`, no `REF` bump**. Edit cli-guard, rebuild ward, done.

- The target errors clearly if `../cli-guard` is missing.
- `go.work` and `go.work.sum` are gitignored on purpose: a committed `use (. ../cli-guard)` would break single-repo builds in CI and the headless `warded` container, where the sibling isn't present. Single-repo builds keep resolving cli-guard from the pinned module version.
- Delete `go.work` to return to the pinned version at any time.

## See also

- [CONTRIBUTING.md](../CONTRIBUTING.md) - the contribution process this workflow supports.
- [README.md](../README.md) - human-facing intro, install steps.
