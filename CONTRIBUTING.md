# Contributing to ward

Thank you for your interest! :wave:

## Where contributions land (read this first)

ward lives on two forges, and which you use depends on who you are.

- **Canonical home: Forgejo** at [`forgejo.coilysiren.me/coilyco-flight-deck/ward`](https://forgejo.coilysiren.me/coilyco-flight-deck/ward) - `main`, issues, and commits live there. Registration is closed.
- **External front door: the GitHub mirror** at [`github.com/coilyco-flight-deck/ward`](https://github.com/coilyco-flight-deck/ward). It mirrors `main` read-only, but it is the surface **you** contribute through.

The path for an outside contributor holding only a GitHub account:

1. **File the issue on the GitHub mirror** via a [new issue](https://github.com/coilyco-flight-deck/ward/issues/new). That's your bug report or feature request and where discussion happens.
2. **Open your PR against the GitHub mirror.** Fork, push a branch, open the PR there. You do not need - and cannot get - a Forgejo account for this.
3. **A maintainer (or warded agent) carries an accepted change to Forgejo.** Because Forgejo is authoritative, an accepted GitHub PR is replayed onto Forgejo `main`, closing the matching Forgejo issue, and your GitHub PR is closed as merged.

The `closes #N` convention below refers to **Forgejo** issue numbers. External PRs can point at the GitHub issue they address.

Maintainers and warded agents on the canonical repo skip the mirror and follow the steps below against Forgejo directly.

## Before you open a PR

1. **Open an issue first.** Every commit closes a same-repo (Forgejo) issue (`closes #N` in the commit body). Discussion happens in the issue, the PR is the change itself. This applies even to trivial fixes, the issue gives the change a stable URL. **External contributors:** file that issue on the GitHub mirror (see [above](#where-contributions-land-read-this-first)).
2. **Stay close to scope.** ward is intentionally small. It exposes a project's dev surface on top of [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard). Features that pull this package out of its lane will get pushed back. Operator and personal-infra verbs belong in the operator CLI, repo-specific Makefile targets belong in the downstream repo's `.ward/ward.yaml`. The cli-guard/ward-kdl/ward boundary is load-bearing - folding cli-guard into ward was [considered and rejected](docs/architecture.md#considered-and-rejected-folding-cli-guard-into-ward), so don't reopen it.
3. **Run the dev verbs before pushing.** Install ward from the centralized flight-deck tap with `brew install coilyco-flight-deck/tap/ward` (tap it first, see [README](README.md#install)), then:

   ```
   ward exec build
   ward exec test
   ward exec vet
   ward exec lint
   ```

   The `.ward/ward.yaml` ↔ Makefile contract is checked by `ward doctor` (its allowlist drift guard) and CI, not a `ward lint` verb - `lint` is only the `ward exec lint` dev verb.

## Extending ward to your stack

**Add a stack by implementing an adapter behind a port, not by editing unrelated packages.** Start from the seam your stack needs, then follow [docs/architecture.md](docs/architecture.md).

1. Split `issueForge`.
2. Add a GitLab forge adapter.
3. Add a podman runtime adapter.
4. Add new agent adapters.

## Working on cli-guard side by side

ward consumes [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) as a separate Go module pinned in `go.mod`. For local dev, `make workspace` resolves it from a sibling checkout with no tag or `go get`. See [docs/workspace.md](docs/workspace.md).

## Code of Conduct

Participation in this community is governed by the [Code of Conduct](CODE_OF_CONDUCT.md), adapted from the [Contributor Covenant 2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/).

## Security disclosures

See [SECURITY.md](SECURITY.md). Do not file vulnerabilities as public issues.
