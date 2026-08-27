---
ward:
  workflow: pull-request
---
# Agent instructions

Read-only surface instructions override the base doctrine below whenever a container or director session says it is read-only. In those sessions, `/workspace` is read-only by default and `/scratch` is the only writable place for throwaway scripts or scratch files.

This file is the self-contained agent base for `ward`. Work from it alone. Ward
does not depend on a private harness chain or host-fleet context.

## Audience: which agent are you?

Two different agents read this file, and only **one** of the two sections below is yours. Pick before you read further.

- **Adopter agent** - you were handed a repo URL and a task like "install ward and set it up for this repo". Read **[Setting up ward for a repo](#setting-up-ward-for-a-repo)** next, follow the ordered path, and stop. Everything from [Scope](#scope) onward is about developing ward's own source and **does not apply to you** - skip it.
- **Contributor agent** - you are changing ward's own Go, docs, or config in this repo. Skip the adopter section. [Scope](#scope) onward is your operating base.

Unsure? You are almost certainly the **adopter**. The delegated "set this up for me" task is the common case, and it needs nothing from the contributor half.

## Setting up ward for a repo

**This section is for the adopter agent.** Adopting ward means wiring the audited verb gate into a target repo. This path is **forge-agnostic**: it works against any git repo (GitHub included) with nothing but Homebrew and a `.ward/ward.yaml`. You need no private harness configuration and no Forgejo account. Maintainer-only context below is not an adoption prerequisite.

The ordered path, start to finish:

1. **Install** - `brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap`, then `brew install coilyco-flight-deck/tap/ward`. The explicit tap URL is required because the tap lives on Forgejo, not github.com. Full steps: [README Install](README.md#install).
2. **Inventory the compact release-era surfaces** - start with [docs/agent-lifecycle.md](docs/agent-lifecycle.md) and [docs/ward-yaml.md](docs/ward-yaml.md).
3. **Use the live gate** - route current dev work through `ward exec build`, `ward exec test`, and the other verbs declared in `.ward/ward.yaml`. To hand-edit or understand a field, use the schema reference in [docs/ward-yaml.md](docs/ward-yaml.md). The live contract is the command allowlist, not the retired setup/doctor surface.
Once that is done, contributors route dev work through the verbs your `.ward/ward.yaml` declares - `ward exec build`, `ward exec test`, and so on. See [docs/exec-verb.md](docs/exec-verb.md).

**Not part of adoption.** Ward's second half - the `warded` / `ward agent` container driver - is Forgejo-locked and owner-gated ([docs/first-run.md](docs/first-run.md)). It is **not** what "set up ward for this repo" asks for, and an external repo cannot drive it without forking. Ignore it unless your task is specifically about running the agent fleet.

## Scope

`ward` is a contributor-facing [umbra](https://forgejo.coilysiren.me/coilyco-flight-deck/umbra) consumer: the gate a contributor (human or agent) routes through to build, test, and lint code.

Ward's canonical vocabulary lives in [docs/terminology.md](docs/terminology.md).
When changing docs, prompts, command output, or agent instructions, use that
page for operational distinctions such as dispatch vs launch, role vs harness,
run vs issue, workflow vs outcome, stop vs reap, drain vs burndown, and release
branch vs published release. Add or update terminology there before spreading a
new synonym through the repo.

Ward uses umbra as its policy engine and keeps its agent, container,
repository-development, and tracker control plane native. External operator
products are outside Ward's runtime and repository contract.

- **Contributor dev verbs** - `build`, `test`, `vet`, `lint`, `tidy`, `cover`, declared per-repo in `.ward/ward.yaml`.
- **Native control-plane verbs** - hand-written commands under `agent`,
  `container`, `git`, and related Ward-owned groups.
- **External operator verbs** - provider-specific commands outside Ward.

## Project shape

Single Go module (path `github.com/coilyco-flight-deck/ward`). CLI at `cmd/ward/`.

## Repo boundaries

- Upstream: `coilyco-flight-deck/umbra` is the policy/routing engine. Thin consumer, not a fork.
- External operator products remain outside this repository.
- Downstream: consumers upgrade to the `ward` binary and `.ward` config on their own schedule.

## Commands

These are the verbs for building ward itself (contributor agents). An adopter runs ward against a **different** repo - see [Setting up ward for a repo](#setting-up-ward-for-a-repo). They live in the [justfile](justfile), not this repo's `.ward/ward.yaml`, per coilysiren/inbox#366. Route through it, not bare go:

- `just build`
- `just test`
- `just test-windows-compile`
- `just vet`
- `just lint`
- `just tidy`

Install via the flight-deck brew tap - see [README.md](README.md).

## Validation

The `justfile` <-> `Makefile` contract is checked by `ward doctor` (no
`ward lint` verb). The pinned pre-commit suite runs every commit.

## Safety

Every invocation validates argv against shell-metacharacter rejection, writes one append-only JSONL audit row, stamps a best-effort `repo_root` audit field, and gates repo verbs on a clean+synced tree. The PreToolUse hook resolves `ward` and `coily` via `command -v` and refuses unless the resolved path is a canonical homebrew location (blocks PATH-hijack). Hard denial stays the job of `permissions.deny`.

## Cross-repo contracts

- Engine: `coilyco-flight-deck/umbra` (pinned via go.mod).
- Pre-commit suite: pinned via `rev:` in `.pre-commit-config.yaml`.
- Downstream config schema: `.ward/ward.yaml` - umbra `repocfg` format, fields in [docs/ward-yaml.md](docs/ward-yaml.md).

## Release

Forgejo is canonical. A push to `main` runs the promote gate, builds the
checksummed binary matrix once as commit-scoped draft assets, refreshes the
container release alias, and fast-forwards `release`. The release workflow
consumes that exact promoted SHA, verifies and publishes the staged bytes
without rebuilding, then updates install channels. The GitHub release is a
verified mirror and public front door, not the canonical release record. Full
contract: [docs/release.md](docs/release.md).

Never write the literal skip-CI token in a commit body or it silently disables the workflow on that push. Describe it as "skip-CI marker".

Post-push, verify the release run on Forgejo Actions through
`ward agent pr runs coilyco-flight-deck/ward --limit 1`. Once green, refresh
the installed Ward binary.

## Agent rules

<!-- BEGIN managed by agentic-os/scripts/apply-git-workflow.py -->
### Git workflow

**This repo runs the `pull-request` lane**, declared as `ward.workflow` in this file's frontmatter. The agent commits to a task branch, pushes it, opens a Forgejo pull request, and stops there. The author does not merge on this lane. The director merge lane takes it from the pull request onward.

The fleet runs two lanes, and both authorize the same core actions:

* `merge-remote-main` - the agent commits, pushes to `main`, and closes the issue. No branch and no pull request.
* `pull-request-and-merge` - the agent commits to a task branch, pushes it, opens a pull request, and merges that pull request itself once it is green.

**Every lane slug names what the AGENT does, never what someone else does.** `pull-request-and-merge` carries the merge because the agent that authored the code merges its own pull request. `pull-request` drops `-and-merge` because the author stops at the pull request and the director merge lane takes over. Reading `pull-request-and-merge` as "someone else merges it later" inverts the two lanes and leaves finished work sitting unmerged.

**These actions are pre-authorized on every lane, and the agent MUST take them without asking first.** Committing, creating a branch, pushing a branch, pushing the lane's own destination, and opening a pull request are ordinary reversible work, not the destructive wall that earns a question. Stopping to ask is how a turn ends with the work stranded in a dirty worktree.

* **ALWAYS commit** in-scope work and **ALWAYS push** it to the canonical remote before pausing, reporting a checkpoint, handing off, or ending a turn. A local-only commit is not a checkpoint.
* **ALWAYS open the pull request** in the same turn as the branch's first push, on every lane except `remote-branch-only`. A pushed branch with no pull request is litter nobody reviews.
* **NEVER `--no-verify`** and **NEVER force-push**. Those two are the real walls, and they stay closed.
* **ALWAYS merge your own pull request on `pull-request-and-merge`**, in the same turn, as soon as it is green. Reporting it as open and awaiting someone is the failure this lane exists to prevent.
* **NEVER merge on `pull-request` or `remote-branch-only`.** Those two stop where they stop, and the director merge lane carries a `pull-request` from there.
<!-- END managed by agentic-os/scripts/apply-git-workflow.py -->

- One issue per discrete additive change. `closes #N` encouraged, not enforced.
- v0.x. Minor API breaks ship in `main` with a note in the commit body. Consumers pin a commit until v1.0.0. Lock the API once downstream consumers settle.
- Never use `--no-verify`.
- **Linking convention.** ward is Forgejo-canonical with a read-only GitHub mirror as the public front door, so a forge link is easy to get backwards. When authoring docs: link **same-repo files with relative paths** (they resolve on both forges), send **external-contributor navigation** (file an issue, open a PR, the front door) to **GitHub**, and point **canonical or infrastructural references** (the brew tap, the container registry, releases, a `ward#N` cross-ref, `closes #N`) at **Forgejo**. Full rule in [docs/forge-linking.md](docs/forge-linking.md).

## Checkout residency

This repo is not in Agent Compose's `repository-plan.yaml`, so it has no
resident checkout under `~/projects/<owner>/`. That is intentional. Work it
from a task-scoped temporary clone, and remove that clone once the work lands.

A temporary root can be purged at any time, so commit and push before pausing,
switching tasks, or ending a session. The remote is the only durable artifact.

## See also

- [README.md](README.md) - human intro.
- [docs/README.md](docs/README.md) - docs, by subsystem.
- [docs/terminology.md](docs/terminology.md) - canonical vocabulary.
- [docs/FEATURES.md](docs/FEATURES.md) - what ships today.
- [docs/architecture.md](docs/architecture.md) - product and authority boundaries.
- [justfile](justfile) - this repo's own dev verbs.
- [.ward/ward.yaml](.ward/ward.yaml) - catalog metadata, and the schema an adopter declares verbs in.
