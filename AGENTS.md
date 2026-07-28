# Agent instructions

Read-only surface instructions override the base doctrine below whenever a container or director session says it is read-only. In those sessions, `/workspace` is read-only by default and `/scratch` is the only writable place for throwaway scripts or scratch files.

This file is the self-contained agent base for `ward`. Work from it alone - it points at nothing outside this repo. Ward does not depend on any broader workspace conventions, and needs no `~/.claude` chain, no `agentic-os-kai`, and no host-fleet context to follow.

## Audience: which agent are you?

Two different agents read this file, and only **one** of the two sections below is yours. Pick before you read further.

- **Adopter agent** - you were handed a repo URL and a task like "install ward and set it up for this repo". Read **[Setting up ward for a repo](#setting-up-ward-for-a-repo)** next, follow the ordered path, and stop. Everything from [Scope](#scope) onward is about developing ward's own source and **does not apply to you** - skip it.
- **Contributor agent** - you are changing ward's own Go, docs, or config in this repo. Skip the adopter section. [Scope](#scope) onward is your operating base.

Unsure? You are almost certainly the **adopter**. The delegated "set this up for me" task is the common case, and it needs nothing from the contributor half.

## Setting up ward for a repo

**This section is for the adopter agent.** Adopting ward means wiring the audited verb gate into a target repo. This path is **forge-agnostic**: it works against any git repo (GitHub included) with nothing but Homebrew and a `.ward/ward.yaml`. You need **no** `~/.claude`, no `agentic-os-kai`, and no Forgejo account. Anything below that mentions those is internal-fleet context for ward's own maintainers, not an adoption prerequisite - skip it.

The ordered path, start to finish:

1. **Install** - `brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap`, then `brew install coilyco-flight-deck/tap/ward`. The explicit tap URL is required because the tap lives on Forgejo, not github.com. Full steps: [README Install](README.md#install).
2. **Inventory the compact release-era surfaces** - start with [docs/agent-lifecycle.md](docs/agent-lifecycle.md) and [docs/ward-yaml.md](docs/ward-yaml.md).
3. **Use the live gate** - route current dev work through `ward exec build`, `ward exec test`, and the other verbs declared in `.ward/ward.yaml`. To hand-edit or understand a field, use the schema reference in [docs/ward-yaml.md](docs/ward-yaml.md). The live contract is the command allowlist, not the retired setup/doctor surface.
Once that is done, contributors route dev work through the verbs your `.ward/ward.yaml` declares - `ward exec build`, `ward exec test`, and so on. See [docs/exec-verb.md](docs/exec-verb.md).

**Not part of adoption.** Ward's second half - the `warded` / `ward agent` container driver - is Forgejo-locked and owner-gated ([docs/first-run.md](docs/first-run.md)). It is **not** what "set up ward for this repo" asks for, and an external repo cannot drive it without forking. Ignore it unless your task is specifically about running the agent fleet.

## Scope

`ward` is a contributor-facing [cli-guard](https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard) consumer: the gate a contributor (human or agent) routes through to build, test, and lint code.

Ward's canonical vocabulary lives in [docs/terminology.md](docs/terminology.md).
When changing docs, prompts, command output, or agent instructions, use that
page for operational distinctions such as dispatch vs launch, role vs harness,
run vs issue, workflow vs outcome, stop vs reap, drain vs burndown, and release
branch vs published release. Add or update terminology there before spreading a
new synonym through the repo.

Ward uses cli-guard as its policy engine and keeps its agent, container,
repository-development, and tracker control plane native. AOS owns specgen and
the separate [`aosguard`](docs/aosguard-boundary.md) operator CLI.

- **Contributor dev verbs** - `build`, `test`, `vet`, `lint`, `tidy`, `cover`, declared per-repo in `.ward/ward.yaml`.
- **Native control-plane verbs** - hand-written commands under `agent`,
  `container`, `git`, and related Ward-owned groups.
- **Operator verbs** - `aosguard ops ...`, authored and shipped by AOS. They
  are not Ward commands.

## Project shape

Single Go module (path `github.com/coilyco-flight-deck/ward`). CLI at `cmd/ward/`.

## Repo boundaries

- Upstream: `coilyco-flight-deck/cli-guard` is the policy/routing engine. Thin consumer, not a fork.
- Operator owner: `coilyco-flight-deck/agentic-os` - specgen inputs and
  AOSguard work land there, not in Ward.
- Downstream: consumers upgrade to the `ward` binary and `.ward` config on their own schedule.

## Commands

These are the verbs for building ward itself (contributor agents). An adopter runs ward against a **different** repo - see [Setting up ward for a repo](#setting-up-ward-for-a-repo). ward dogfoods itself. Route through it, not bare go:

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

Forgejo-canonical, on Forgejo Actions not GitHub. Push to `main` runs `.forgejo/workflows/release.yml`: `tag-bump` (minor bump; major hand-driven) + `create-release`, then `bump-tap-formula` rewrites the tap's formula `url`+`sha256` to the new tag (skip-CI marked), failing loudly if the write does not land. The GitHub mirror is a **read-only front door** - canonical `main`, tags, releases, and changelog all live on Forgejo. ward no longer runs an Actions workflow to mirror refs (the former `mirror-to-github.yml` is removed), so keeping the mirror's git refs current is out-of-band, not a release-pipeline step. The pipeline does best-effort publish the binary matrix to a same-tag GitHub release when `GITHUB_MIRROR_PAT` is set (unset, it skips loudly and the Forgejo release is unaffected).

Never write the literal skip-CI token in a commit body or it silently disables the workflow on that push. Describe it as "skip-CI marker".

Post-push at +120s, verify the release run on Forgejo Actions (not the GitHub mirror): `aosguard ops forgejo tasks list coilyco-flight-deck ward --limit 1`. Once green, refresh the installed ward binary.

## Agent rules

- One issue per discrete additive change. `closes #N` encouraged, not enforced.
- v0.x. Minor API breaks ship in `main` with a note in the commit body. Consumers pin a commit until v1.0.0. Lock the API once downstream consumers settle.
- Never use `--no-verify`.
- **Linking convention.** ward is Forgejo-canonical with a read-only GitHub mirror as the public front door, so a forge link is easy to get backwards. When authoring docs: link **same-repo files with relative paths** (they resolve on both forges), send **external-contributor navigation** (file an issue, open a PR, the front door) to **GitHub**, and point **canonical or infrastructural references** (the brew tap, the container registry, releases, a `ward#N` cross-ref, `closes #N`) at **Forgejo**. Full rule in [docs/forge-linking.md](docs/forge-linking.md).

## See also

- [README.md](README.md) - human intro.
- [docs/README.md](docs/README.md) - docs, by subsystem.
- [docs/terminology.md](docs/terminology.md) - canonical vocabulary and analogy bank.
- [docs/FEATURES.md](docs/FEATURES.md) - what ships today.
- [docs/features-release-tooling.md](docs/features-release-tooling.md) - cross-repo tooling and release convention.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
