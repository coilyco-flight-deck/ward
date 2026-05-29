# Agent instructions

Workspace conventions load globally via `~/.claude/CLAUDE.md` -> `agentic-os-kai/AGENTS.md`. This file covers only what's specific to this repo.

The repo, Go module, and Homebrew tap still carry the historical `agent-guard` slug. The produced binary is `ward`, the per-repo config dir is `.ward/`, and the project framing is "ward". The remote rename is deferred and tracked upstream.

## Scope

`ward` is coilysiren's contributor-facing [cli-guard](https://github.com/coilysiren/cli-guard) consumer: the gate a contributor (human or agent) routes through to build, test, and lint coilysiren code. It carries coilysiren's dev verbs.

The boundary between ward and [coily](https://github.com/coilysiren/coily) is load-bearing, but it is split by role, not by audience:

- **coily is the operator CLI.** Anything touching Kai's homelab, vault, AWS account, deploy hooks, or other personal infra belongs in coily. ward never grows ops verbs.
- **ward is the contributor gate.** It exposes the dev surface a contributor needs in a coilysiren repo (`build`, `test`, `vet`, `lint`, `tidy`, `cover`). coilysiren-specific dev verbs are welcome here. Repo-specific Makefile targets are declared per-repo in `.ward/ward.yaml`.

## Project shape

Single Go module (path `github.com/coilysiren/agent-guard`, unchanged). CLI at `cmd/ward/`. Per-repo config lives downstream as `.ward/ward.yaml`. Homebrew formula in-tree at `Formula/ward.rb`.

## Repo boundaries

- Upstream: `coilysiren/cli-guard` provides the policy/routing engine. Thin consumer, not a fork.
- Sibling: `coilysiren/coily` is the operator-verbs counterpart. coily-land doesn't cross over.
- Downstream: coilysiren-owned consumers, upgraded to the `ward` binary and `.ward` config on their own schedule.

## Commands

ward dogfoods itself. Route through it, not bare go:

- `ward exec build`
- `ward exec test`
- `ward exec vet`
- `ward exec lint`
- `ward exec tidy`

Install: `brew tap coilysiren/agent-guard https://forgejo.coilysiren.me/coilysiren/agent-guard && brew install coilysiren/agent-guard/ward`.

## Validation

The `.ward/ward.yaml` <-> `Makefile` contract is checked by `ward lint`. The cross-repo pre-commit suite from `coilysiren/agentic-os` runs every commit.

## Safety

Every invocation validates argv against shell-metacharacter rejection, writes one append-only JSONL audit row, binds to a git toplevel via `--commit-scope`, and refuses repo-shaped verbs on a dirty tree. The PreToolUse hook resolves `ward` and `coily` via `command -v` and refuses unless the resolved path is a canonical homebrew location (blocks PATH-hijack). Hard denial stays the job of `permissions.deny`.

## Cross-repo contracts

- Engine: `coilysiren/cli-guard` (pinned via go.mod).
- Pre-commit suite: `coilysiren/agentic-os` (pinned via `rev:` in `.pre-commit-config.yaml`).
- Downstream config schema: `.ward/ward.yaml`. Schema lives here.

## Release

Push to `main` triggers `.github/workflows/release.yml`: tag-action computes semver (patch default, conventional commits drive minor/major), cuts a GH Release, then `bump-formula` rewrites the formula's url+tag+revision via the Contents API and pushes back with a skip-CI marker. `Formula/ward.rb` is source of truth.

Never write the literal skip-CI token in a commit body or you'll silently disable the release workflow on that push (GitHub greps the entire message). Quote as "skip-CI marker" if you need to describe it.

Post-push: verify CI at +120s (`coily ops gh run list --repo coilysiren/agent-guard --limit 1`). Once green: `brew upgrade coilysiren/agent-guard/ward`.

## Agent rules

- One issue per discrete additive change. Every commit closes one with `closes #N`.
- v0.x. Minor API breaks ship in `main` with a note in the commit body. Consumers pin a commit until v1.0.0. Lock the API once the downstream consumers settle.
- Never use `--no-verify`.

## See also

- [README.md](README.md) - human-facing intro.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
