# Agent instructions

Workspace conventions load globally via `~/.claude/CLAUDE.md` -> `agentic-os-kai/AGENTS.md`. This file covers only what's specific to this repo.

## Scope

`ward` is a contributor-facing [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) consumer: the gate a contributor (human or agent) routes through to build, test, and lint project code. It carries the project's dev verbs.

ward is also absorbing the operator surface: [coily](https://github.com/coilyco-bridge/coily) is retiring and its ops verbs fold into ward, retiring the old "ward never grows ops verbs" boundary. Two verb kinds now:

- **Contributor dev verbs** - `build`, `test`, `vet`, `lint`, `tidy`, `cover`, declared per-repo in `.ward/ward.yaml`.
- **Operator verbs** - from coily. Spec REST rides ward-kdl (`ops forgejo`, `ops aws`); composite control flow is hand-written gated Go in `cmd/ward`. `ward ci watch` (the native CI watcher, ward#88) is one such verb; see [docs/ci-watch.md](docs/ci-watch.md).

## Project shape

Single Go module (path `github.com/coilyco-flight-deck/ward`). CLI at `cmd/ward/`. Per-repo config lives downstream as `.ward/ward.yaml`.

## Repo boundaries

- Upstream: `coilyco-flight-deck/cli-guard` provides the policy/routing engine. Thin consumer, not a fork.
- Retiring sibling: `coilyco-bridge/coily` is the operator CLI being wound down; its ops verbs migrate into ward. New operator work lands here, not in coily.
- Downstream: consumers upgraded to the `ward` binary and `.ward` config on their own schedule.

## Commands

ward dogfoods itself. Route through it, not bare go:

- `ward exec build`
- `ward exec test`
- `ward exec vet`
- `ward exec lint`
- `ward exec tidy`

Install: `brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap && brew install coilyco-flight-deck/tap/ward`.

## Validation

The `.ward/ward.yaml` <-> `Makefile` contract is checked by `ward lint`. The cross-repo pre-commit suite from `coilyco-flight-deck/agentic-os` runs every commit.

## Safety

Every invocation validates argv against shell-metacharacter rejection, writes one append-only JSONL audit row, stamps a best-effort `repo_root` audit field, and gates repo verbs on a clean+synced tree. The PreToolUse hook resolves `ward` and `coily` via `command -v` and refuses unless the resolved path is a canonical homebrew location (blocks PATH-hijack). Hard denial stays the job of `permissions.deny`.

## Cross-repo contracts

- Engine: `coilyco-flight-deck/cli-guard` (pinned via go.mod).
- Pre-commit suite: `coilyco-flight-deck/agentic-os` (pinned via `rev:` in `.pre-commit-config.yaml`).
- Downstream config schema: `.ward/ward.yaml`. Schema lives here.

## Release

Forgejo-canonical, on Forgejo Actions not GitHub. Push to `main` runs `.forgejo/workflows/release.yml`: `tag-bump` (minor bump; major hand-driven) + `create-release`, then `bump-tap-formula` rewrites the tap's formula `url`+`sha256` to the new tag (skip-CI marked), failing loudly if the write does not land. `mirror-to-github.yml` mirrors main + tags to the read-only GitHub mirror.

Never write the literal skip-CI token in a commit body or it silently disables the workflow on that push. Describe it as "skip-CI marker".

Post-push: verify CI at +120s (`coily ops gh run list --repo coilyco-flight-deck/ward --limit 1`). Once green: `brew upgrade coilyco-flight-deck/tap/ward`.

## Agent rules

- One issue per discrete additive change. `closes #N` encouraged, not enforced.
- v0.x. Minor API breaks ship in `main` with a note in the commit body. Consumers pin a commit until v1.0.0. Lock the API once the downstream consumers settle.
- Never use `--no-verify`.

## See also

- [README.md](README.md) - human-facing intro.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
