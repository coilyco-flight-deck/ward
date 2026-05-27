# Agent instructions

Workspace conventions load globally via `~/.claude/CLAUDE.md` -> `agentic-os-kai/AGENTS.md`. This file covers only what's specific to this repo.

## Scope

A generic [cli-guard](https://github.com/coilysiren/cli-guard) consumer for repos with external contributors, where [coily](https://github.com/coilysiren/coily)'s Kai-specific verbs would be inappropriate. Sibling concept to coily, separated by audience, not mechanism.

The boundary between agent-guard and coily is load-bearing:

- **No personal verbs.** Anything touching Kai's homelab, vault, AWS account, or other personal infra belongs in coily. If a verb wouldn't make sense to a stranger cloning a downstream repo, it doesn't ship here.
- **No repo-specific verbs.** agent-guard exposes the generic dev surface (`build`, `test`, `vet`, `lint`, `tidy`). Repo-specific Makefile targets stay in the downstream repo's `.agent-guard/agent-guard.yaml`.

## Project shape

Single Go module. CLI at `cmd/agent-guard/`. Per-repo config lives downstream as `.agent-guard/agent-guard.yaml`. Homebrew formula in-tree at `Formula/agent-guard.rb`.

## Repo boundaries

- Upstream: `coilysiren/cli-guard` provides the policy/routing engine. Thin consumer, not a fork.
- Sibling: `coilysiren/coily` is the personal-verbs counterpart. coily-land doesn't cross over.
- First downstream adopters: `cli-mcp`, `cli-web-docs`, `cli-web-ops`.

## Commands

agent-guard dogfoods itself. Route through it, not bare go:

- `agent-guard exec build`
- `agent-guard exec test`
- `agent-guard exec vet`
- `agent-guard exec lint`
- `agent-guard exec tidy`

Install: `brew tap coilysiren/agent-guard https://forgejo.coilysiren.me/coilysiren/agent-guard && brew install coilysiren/agent-guard/agent-guard`.

## Validation

The `.agent-guard/agent-guard.yaml` <-> `Makefile` contract is checked by `agent-guard lint`. The cross-repo pre-commit suite from `coilysiren/agentic-os` runs every commit.

## Safety

Every invocation validates argv against shell-metacharacter rejection, writes one append-only JSONL audit row, binds to a git toplevel via `--commit-scope`, and refuses repo-shaped verbs on a dirty tree. The PreToolUse hook resolves `agent-guard` and `coily` via `command -v` and refuses unless the resolved path is a canonical homebrew location (blocks PATH-hijack). Hard denial stays the job of `permissions.deny`.

## Cross-repo contracts

- Engine: `coilysiren/cli-guard` (pinned via go.mod).
- Pre-commit suite: `coilysiren/agentic-os` (pinned via `rev:` in `.pre-commit-config.yaml`).
- Downstream config schema: `.agent-guard/agent-guard.yaml`. Schema lives here.

## Release

Push to `main` triggers `.github/workflows/release.yml`: tag-action computes semver (patch default, conventional commits drive minor/major), cuts a GH Release, then `bump-formula` rewrites the formula's url+tag+revision via the Contents API and pushes back with a skip-CI marker. `Formula/agent-guard.rb` is source of truth.

Never write the literal skip-CI token in a commit body or you'll silently disable the release workflow on that push (GitHub greps the entire message). Quote as "skip-CI marker" if you need to describe it.

Post-push: verify CI at +120s (`coily ops gh run list --repo coilysiren/agent-guard --limit 1`). Once green: `brew upgrade coilysiren/agent-guard/agent-guard`.

## Agent rules

- One issue per discrete additive change. Every commit closes one with `closes #N`.
- v0.x. Minor API breaks ship in `main` with a note in the commit body. Consumers pin a commit until v1.0.0. Lock the API once a second adopter lands beyond the urfave/cli repos.
- Never use `--no-verify`.

## See also

- [README.md](README.md) - human-facing intro.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.agent-guard/agent-guard.yaml](.agent-guard/agent-guard.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
