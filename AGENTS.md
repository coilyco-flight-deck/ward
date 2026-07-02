# Agent instructions

This file is the self-contained agent base for `ward`. Work from it alone - it points at nothing outside this repo. On the maintainer's own hosts, broader workspace conventions load globally underneath it, but ward depends on none of them.

## Scope

`ward` is a contributor-facing [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) consumer: the gate a contributor (human or agent) routes through to build, test, and lint code.

ward also carries the operator surface from the retiring [coily](https://github.com/coilyco-bridge/coily). Three roles, by **when** they run: cli-guard the **engine**, [ward-kdl](docs/ward-kdl.md) the **build-time generator** (guardfile in, audited CLI out), `ward` the **run-time product** that embeds those surfaces. Two verb kinds:

- **Contributor dev verbs** - `build`, `test`, `vet`, `lint`, `tidy`, `cover`, declared per-repo in `.ward/ward.yaml`.
- **Operator verbs** - [ward-kdl](docs/ward-kdl.md) generates the `ward ops <api>` REST surfaces (`ops forgejo`, `ops aws`). Composite control flow stays hand-written Go in `cmd/ward` (e.g. `ward agent`, `ward container reap`).

## Project shape

Single Go module (path `github.com/coilyco-flight-deck/ward`). CLI at `cmd/ward/`.

## Repo boundaries

- Upstream: `coilyco-flight-deck/cli-guard` is the policy/routing engine. Thin consumer, not a fork.
- Retiring sibling: `coilyco-bridge/coily` - ops verbs migrate into ward. New operator work lands here, not coily.
- Downstream: consumers upgrade to the `ward` binary and `.ward` config on their own schedule.

## Commands

ward dogfoods itself. Route through it, not bare go:

- `ward exec build`
- `ward exec test`
- `ward exec vet`
- `ward exec lint`
- `ward exec tidy`

Install via the flight-deck brew tap - see [README.md](README.md).

## Validation

The `.ward/ward.yaml` <-> `Makefile` contract is checked by `ward doctor` (no `ward lint` verb). The cross-repo pre-commit suite from `coilyco-flight-deck/agentic-os` runs every commit.

## Safety

Every invocation validates argv against shell-metacharacter rejection, writes one append-only JSONL audit row, stamps a best-effort `repo_root` audit field, and gates repo verbs on a clean+synced tree. The PreToolUse hook resolves `ward` and `coily` via `command -v` and refuses unless the resolved path is a canonical homebrew location (blocks PATH-hijack). Hard denial stays the job of `permissions.deny`.

## Cross-repo contracts

- Engine: `coilyco-flight-deck/cli-guard` (pinned via go.mod).
- Pre-commit suite: `coilyco-flight-deck/agentic-os` (pinned via `rev:` in `.pre-commit-config.yaml`).
- Downstream config schema: `.ward/ward.yaml` - cli-guard `repocfg` format, fields in [docs/ward-yaml.md](docs/ward-yaml.md).

## Release

Forgejo-canonical, on Forgejo Actions not GitHub. Push to `main` runs `.forgejo/workflows/release.yml`: `tag-bump` (minor bump; major hand-driven) + `create-release`, then `bump-tap-formula` rewrites the tap's formula `url`+`sha256` to the new tag (skip-CI marked), failing loudly if the write does not land. `mirror-to-github.yml` mirrors refs only; releases stay on Forgejo.

Never write the literal skip-CI token in a commit body or it silently disables the workflow on that push. Describe it as "skip-CI marker".

Post-push at +120s, verify the release run on Forgejo Actions (not the GitHub mirror): `ward ops forgejo tasks list coilyco-flight-deck ward --limit 1`. Once green: `brew upgrade coilyco-flight-deck/tap/ward`.

## Agent rules

- One issue per discrete additive change. `closes #N` encouraged, not enforced.
- v0.x. Minor API breaks ship in `main` with a note in the commit body. Consumers pin a commit until v1.0.0. Lock the API once downstream consumers settle.
- Never use `--no-verify`.

## See also

- [README.md](README.md) - human intro.
- [docs/README.md](docs/README.md) - docs, by subsystem.
- [docs/FEATURES.md](docs/FEATURES.md) - what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
