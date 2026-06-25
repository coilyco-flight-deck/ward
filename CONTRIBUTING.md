# Contributing to ward

Thank you for your interest! :wave:

This project is run on volunteer time, so please have patience.

## Before you open a PR

1. **Open an issue first.** Every commit in this repo closes a same-repo issue (`closes #N` in the commit body). Discussion happens in the issue, the PR is the change itself. This applies even to trivial fixes, the issue gives the change a stable URL.
2. **Stay close to scope.** ward is intentionally small. It exposes a project's dev surface on top of [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard). Features that pull this package out of its lane will get pushed back. Operator and personal-infra verbs belong in the operator CLI, repo-specific Makefile targets belong in the downstream repo's `.ward/ward.yaml`. The cli-guard/ward-kdl/ward boundary is load-bearing, not incidental - folding cli-guard into ward was [considered and rejected](docs/architecture.md#considered-and-rejected-folding-cli-guard-into-ward), so don't reopen it.
3. **Run the dev verbs before pushing.** Install ward from the centralized flight-deck tap with `brew install coilyco-flight-deck/tap/ward` (tap it first, see [README](README.md#install)), then:

   ```
   ward exec build
   ward exec test
   ward exec vet
   ward exec lint
   ```

   The `.ward/ward.yaml` ↔ Makefile contract is checked by `ward lint` and by CI on every push.

## Working on cli-guard side by side (Go workspace)

ward consumes [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) as a separate Go module, pinned in `go.mod` (and via the Makefile `REF` for the `specverb-gen` driver). Without a workspace, every cli-guard change you want to use here costs a full cross-module release: tag + release cli-guard, `go get` the new version into ward's `go.mod`, bump the Makefile `REF`, *then* the change is usable.

A Go workspace collapses that loop for local dev. With cli-guard checked out beside ward:

```
ward/         <- you are here
cli-guard/    <- ../cli-guard
```

run:

```
make workspace
```

That writes a **gitignored** `go.work` (`use (. ../cli-guard)`). cli-guard now resolves from your local working tree for `go build`, `ward exec`, and `specverb-gen` runs — **no tag, no `go get`, no `REF` bump**. Edit cli-guard, rebuild ward, done.

- The target errors clearly if `../cli-guard` is missing.
- `go.work` and `go.work.sum` are gitignored on purpose: a committed `use (. ../cli-guard)` would break single-repo builds in CI and the headless `warded` container, where the sibling isn't present. Single-repo builds keep resolving cli-guard from the pinned module version.
- Delete `go.work` to return to the pinned version at any time.

## Code of Conduct

Participation in this community is governed by the [Code of Conduct](CODE_OF_CONDUCT.md), adapted from the [Contributor Covenant 2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/).

## Security disclosures

See [SECURITY.md](SECURITY.md). Do not file vulnerabilities as public issues.
