# Agent instructions

Workspace-level conventions (git workflow, test/lint autonomy, readonly ops, writing voice) are loaded globally via `~/.claude/CLAUDE.md` → `coilyco-ai/AGENTS.md`. This file covers only what's specific to this repo.

## What agent-guard is

A generic-purpose [cli-guard](https://github.com/coilysiren/cli-guard) consumer. Built for repos with external contributors, where coily's Kai-specific verbs would be inappropriate. Sibling concept to [coily](https://github.com/coilysiren/coily), separated by audience, not by mechanism.

## Scope discipline

The boundary between agent-guard and coily is the load-bearing design choice. Two rules:

- **No personal verbs.** Anything that touches Kai's homelab, vault, AWS account, or other personal infrastructure belongs in coily, not here. If a verb would not make sense to a stranger cloning a downstream repo, it does not ship here.
- **No repo-specific verbs.** agent-guard exposes the generic dev surface (`build`, `test`, `vet`, `lint`, `tidy`). Repo-specific Makefile targets stay in the downstream repo's `.agent-guard/agent-guard.yaml`.

## Dev verbs

agent-guard dogfoods itself. Once installed (`brew install coilysiren/tap/agent-guard`), route through it, not bare go:

- `agent-guard exec build`
- `agent-guard exec test`
- `agent-guard exec vet`
- `agent-guard exec lint`
- `agent-guard exec tidy`

The `.coily/coily.yaml` ↔ `Makefile` contract is checked by `agent-guard lint`. Kai's own machines also accept `coily exec ...` against the same allowlist.

## v0 API discipline

v0.x. Minor API breaks ship in `main` with a note in the commit body. Consumers pin a specific commit until v1.0.0. Once a second downstream adopter lands beyond the urfave/cli repos, lock the API and bump.

## Filing issues

One issue per discrete additive change. Every commit closes a same-repo issue with `closes #N`.
