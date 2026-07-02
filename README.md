# ward

**ward wraps a project's dev verbs - `build`, `test`, `vet`, `lint`, `tidy`, `cover` - behind a policy gate, so nothing reaches `make` or `go` unchecked.** Every run validates its own arguments, appends one line to an audit log, and is refused if it could not be reconstructed from git history. It is the single command a contributor (human or agent) routes build-and-test work through. All it needs is a repo with a `.ward/ward.yaml` and Homebrew.

ward has a second half for running coding agents: `ward agent` drives a harness (claude, codex, goose, ...) into a throwaway container to carry a Forgejo issue from fresh clone to merged `main`, its reach bounded by that container. That surface is exposed as **`warded`**, a thin symlink onto `ward agent`. The name, the three-layer split it sits on, and the operator surface it absorbs are covered below and in [`docs/architecture.md`](docs/architecture.md).

## Who it's for

- **A contributor (human or agent)** who wants every `build` / `test` / `lint` run argv-validated, audited, and gated on a clean tree - one wrapper instead of bare `make` / `go` / `gh` / `aws`. This half is forge-agnostic: point it at any repo.
- **An operator** running an autonomous agent fleet who wants each run boxed in a throwaway container, its reach bounded by an allowlist, and its whole session recorded - not a trusted shell.

If you just want the audited verb gate, you need nothing but a repo and Homebrew. The agent driver and operator surfaces assume the forge ward is built around.

## What it requires

- **macOS or Linux + Homebrew** to install the binary (see [Install](#install)).
- **A Forgejo instance** for the agent driver (`warded` / `ward agent`) and the operator surface (`ward ops forgejo`). ward is **Forgejo-canonical**: it carries Forgejo issues and pushes to a Forgejo `main`. The GitHub mirror is read-only and PR-gated. A GitHub-only shop can still use the local verb gate, but the agent and ops surfaces target Forgejo.
- **Docker** for the container agent flow - each `warded` run boots an ephemeral container, configures forge git auth inside it, runs the agent, and reaps it. See [`docs/container.md`](docs/container.md).

The plain verb gate (`ward exec`, `ward git`, `ward pkg`, `ward audit`) needs none of the above - just the repo and its `.ward/ward.yaml`.

## What it does

Wraps a project's dev verbs (`build`, `test`, `vet`, `lint`, `tidy`, `cover`) behind cli-guard's policy gate. Every invocation validates argv against a shell-metacharacter policy, writes one append-only JSONL audit row to `~/.ward/audit/<repo>.jsonl`, and gates repo verbs on a clean-and-synced tree so the row can be reconstructed from git history. See [`docs/exec-verb.md`](docs/exec-verb.md).

Each repo declares which Makefile targets are exposed in `.ward/ward.yaml`, and `ward doctor` verifies the two surfaces have not drifted. The contract is stricter than "the target name exists": each exposed Makefile target must carry a `## <description>` help comment (the self-documenting-Makefile convention, e.g. `build: ## Build all packages.`) whose text equals the command's `description:`, and `run:` must be exactly `make <name>`. A bare `target:` recipe with no `## ...` comment is not registered, so `ward doctor` reports it as unmatched even though `make <target>` runs by hand. See [`docs/doctor.md`](docs/doctor.md) (Allowlist contract).

## The gate says no

ward is a security boundary, so the interesting demo is not what it runs - it is what it **refuses**. The clean-tree gate declines a verb when the run could not be reconstructed from history, and the argv policy declines anything carrying shell metacharacters:

```
$ ward exec test                 # on a branch with no upstream set
ward exec test: refused - repo verb gated on a clean, synced tree
  reason: HEAD has no synced upstream (push or set upstream first)
  the audit row must be reconstructable from committed history
  override for a genuine emergency: ward --audit-override-dirty exec test

$ ward exec test -- -run 'Foo; rm -rf /'
ward exec test: refused - argument "Foo; rm -rf /" contains a shell metacharacter
```

The override exists, but it is loud: the audit row is stamped `audit_override=true` with the full working-tree status, so an emergency bypass is still reconstructable after the fact. Denial is the default posture, not an error path. See [`docs/exec-verb.md`](docs/exec-verb.md) and [`docs/agent-gate.md`](docs/agent-gate.md).

## Install

Install from the centralized flight-deck tap:

```
brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap
brew install coilyco-flight-deck/tap/ward
```

The explicit-URL form is required because the tap lives on forgejo, not github.com. The formula installs `ward` and the spec-driven `ward-kdl` (both stamped with the release tag) plus the `warded` symlink. Upgrade with `ward upgrade`.

**Releases live on Forgejo.** This repo is canonical on [forgejo.coilysiren.me/coilyco-flight-deck/ward](https://forgejo.coilysiren.me/coilyco-flight-deck/ward); the github.com copy is a read-only mirror of `main` + tags only, so its Releases page is intentionally empty - see the [canonical releases](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/releases) for the current version and changelog.

## Usage

The audited verb gate, on any repo:

```
ward exec build          # run a declared dev verb through the gate
ward exec test
ward doctor              # check .ward/ward.yaml vs the Makefile (allowlist drift)
ward git commit -m ...   # concurrency-safe, audited git
ward pkg brew bundle     # audited brew wrapper
ward audit tail --follow # stream the audit log
```

The agent driver, against a Forgejo issue. `warded` is a thin symlink onto `ward agent` - read it as a protective circle, the container bounding the agent's reach, not "warded off":

```
warded #98               # put an engineer on issue #98, fire-and-forget
warded engineer #98      # ...spelled out; the engineer role runs detached
warded director --org coilyco-flight-deck   # a heartbeat that drains a backlog lane
warded advisor #98       # answer/triage a ref, writing no code
```

See [`docs/FEATURES.md`](docs/FEATURES.md) for the full verb list.

## Three layers, told apart by when they run

`ward` absorbs the operator surface from the retiring [coily][coily]. The pieces are easiest to keep straight by **when** each runs:

- **[cli-guard][cli-guard]** - the **engine**. The policy-and-routing framework ward consumes (pinned via go.mod). Thin consumer, not a fork.
- **[`ward-kdl`](docs/ward-kdl.md)** - the **build-time generator**. Compiles a KDL guardfile into an audited CLI: the `ward ops <api>` REST surfaces (forgejo, aws, tailscale, ...) shipped as `ward-kdl-{read,write,admin}` tiers.
- **`ward`** - the **run-time product**. Embeds those generated surfaces and adds the `agent` + `exec` layers. Composite control flow (the `agent` roster, `git`) stays hand-written Go.

See [`docs/architecture.md`](docs/architecture.md).

## The Claude Code PreToolUse hook

`ward hook pre-tool-use` is a stdin-driven [Claude Code hook](https://docs.claude.com/en/docs/claude-code/hooks). It refuses `ward`/`coily` unless `command -v` resolves to a canonical homebrew path (blocking PATH-hijack), and catches bare wrapped binaries (`make`, `gh`, `aws`, ...) to name the right wrapper. No network, no state - failures pass through silently, and hard denial stays the job of `permissions.deny`. Register it with `ward install-hooks`. See [`docs/hook.md`](docs/hook.md).

## Where to go next

Over 60 pages under [`docs/`](docs/) cover each surface. The anchors:

- **The verb gate** - [exec-verb.md](docs/exec-verb.md) (the gate), [verb-fallback.md](docs/verb-fallback.md), [git-verbs.md](docs/git-verbs.md), [audit.md](docs/audit.md), [doctor.md](docs/doctor.md), [install-hooks.md](docs/install-hooks.md).
- **The agent driver** - [agent.md](docs/agent.md) (start here), the roster [agent-engineer.md](docs/agent-engineer.md) / [agent-director.md](docs/agent-director.md) / [agent-advisor.md](docs/agent-advisor.md), the [agent-gate.md](docs/agent-gate.md), [agent-credentials.md](docs/agent-credentials.md), [agent-observability.md](docs/agent-observability.md).
- **The container** - [container.md](docs/container.md), [container-reap.md](docs/container-reap.md) (land-or-salvage on teardown), [container-multi-repo.md](docs/container-multi-repo.md), [container-substrate.md](docs/container-substrate.md).
- **Operator surface (ward-kdl / ops)** - [ward-kdl.md](docs/ward-kdl.md), [ward-kdl-tiers.md](docs/ward-kdl-tiers.md), [ops-forgejo.md](docs/ops-forgejo.md).
- **Build & release** - [homebrew-build.md](docs/homebrew-build.md), [release.md](docs/release.md), [github-mirror.md](docs/github-mirror.md), [golangci.md](docs/golangci.md).

## Status

v0.x. Downstream consumers upgrade to the `ward` binary and `.ward` config on their own schedule. Minor API breaks ship in `main` with a note in the commit body; pin a commit until v1.0.0.

## Related

- [cli-guard][cli-guard] - the underlying security-boundary framework.
- [coily][coily] - the operator-facing cli-guard consumer whose surface ward absorbs.
- [cli-mcp][cli-mcp] - a sibling cli-guard consumer that projects a urfave/cli tree as an MCP server.
- [comparison-openshell.md](docs/comparison-openshell.md) - ward vs NVIDIA OpenShell: a verb-level gate, not a kernel sandbox.

## Support

Bug or feature request: [create a new issue][new-issue]. Conduct: [Code of Conduct](CODE_OF_CONDUCT.md). Security: [SECURITY.md](SECURITY.md). License: [`LICENSE`](./LICENSE).

[cli-guard]: https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard
[coily]: https://github.com/coilyco-bridge/coily
[cli-mcp]: https://github.com/coilysiren/cli-mcp
[new-issue]: https://github.com/coilyco-flight-deck/ward/issues/new/choose

## See also

- [docs/README.md](docs/README.md) - the docs index: every doc grouped by subsystem.
- [docs/architecture.md](docs/architecture.md) - ward in three layers (cli-guard, ward-kdl, ward).
- [AGENTS.md](AGENTS.md) - agent-facing operating rules.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
