# Agent instructions

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
2. **Author the config** - from the **target repo root**, run `ward setup`. It scaffolds `.ward/ward.yaml` from that repo's Makefile (one dev verb per `## `-commented target) and writes a commented-out `security:` scaffold. Prune the verbs you do not want to expose. To hand-edit or understand a field, use the schema reference in [docs/ward-yaml.md](docs/ward-yaml.md). What `setup` does: [docs/setup.md](docs/setup.md).
3. **Validate** - run `ward doctor`. It is the loud validator: it checks `.ward/ward.yaml` against the Makefile (allowlist drift) and summarizes the parsed `security:` block, failing hard on a malformed config. There is **no `ward lint` verb** - `doctor` is the validation step, and you should re-run it after every edit to the config. Details: [docs/doctor.md](docs/doctor.md).
4. **Install the hook** (Claude Code only) - run `ward install-hooks` to register the PreToolUse hook in `.claude/settings.json`, so a bare `make` / `gh` / `aws` call gets routed back through the gate. Other harnesses get no host-side hook, so this step is a no-op for them. Details: [docs/install-hooks.md](docs/install-hooks.md).

Once that is done, contributors route dev work through the verbs your `.ward/ward.yaml` declares - `ward exec build`, `ward exec test`, and so on. See [docs/exec-verb.md](docs/exec-verb.md).

**Not part of adoption.** Ward's second half - the `warded` / `ward agent` container driver - is Forgejo-locked and owner-gated ([docs/first-run.md](docs/first-run.md)). It is **not** what "set up ward for this repo" asks for, and an external repo cannot drive it without forking. Ignore it unless your task is specifically about running the agent fleet.

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
