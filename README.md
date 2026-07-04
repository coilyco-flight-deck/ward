# ward

**ward wraps a project's dev verbs - `build`, `test`, `vet`, `lint`, `tidy`, `cover` - behind a policy gate, so nothing reaches `make` or `go` unchecked.** Every run validates its own arguments, appends one line to an audit log, and is refused if it could not be reconstructed from git history. This half is **forge-agnostic**: point it at any git repo - GitHub included - with nothing but a `.ward/ward.yaml` and Homebrew, no forge account of any kind. Only ward's second half, the agent driver below, is tied to a specific forge.

ward's second half is a **guarded execution layer for coding agents**. `ward agent` launches a subscription-authenticated coding CLI (claude, codex, goose, ...) into an ephemeral, least-access container and drives it through an issue-to-merge workflow, its reach bounded by repo-scoped credentials, cli-guard policy, and a durable audit trail. Internally that half is a **manifest-backed harness driver** - it knows how to launch each agent through its own CLI dialect - but the external product is the governed execution layer around it, not the driver. Its landing policy is selectable per run (`--workflow direct-main|pr|patch-only`, see [`docs/agent-workflow.md`](docs/agent-workflow.md)). That surface is exposed as **`warded`**, a thin symlink onto `ward agent`, and sits on the three-layer split covered below and in [`docs/architecture.md`](docs/architecture.md).

## Who it's for

- **A contributor (human or agent)** who wants every `build` / `test` / `lint` run argv-validated, audited, and gated on a clean tree - one wrapper instead of bare `make` / `go` / `gh` / `aws`. Forge-agnostic: point it at any repo.
- **An operator** running an autonomous agent fleet who wants each run boxed in a throwaway container, its reach bounded by an allowlist, and its whole session recorded - not a trusted shell.

## What it requires

- **macOS or Linux + Homebrew** to install the binary (see [Install](#install)).
- **A Forgejo instance** for the agent driver (`warded` / `ward agent`) and the operator surface (`ward ops forgejo`). ward is **Forgejo-canonical**: it carries Forgejo issues and pushes to a Forgejo `main`, and the GitHub mirror is read-only and PR-gated. Which Forgejo, exactly? See the note below the list.
- **Docker** for the container agent flow - each `warded` run boots an ephemeral container, configures forge git auth inside it, runs the agent, and reaps it. The first run pulls one image, `forgejo.coilysiren.me/coilyco-flight-deck/agentic-os:latest` (anonymous pull, no login). See [`docs/container.md`](docs/container.md) for the registry, tag policy, and how to pin off the moving tag.

The plain verb gate (`ward exec`, `ward git`, `ward pkg`, `ward audit`) needs none of the above - just the repo and its `.ward/ward.yaml`.

**Which Forgejo? As shipped, ward targets one - `forgejo.coilysiren.me` - and only `coily*`-owned orgs.** The API endpoint, the private coilyco-ops SSM token path, and an `owner matches coily*` gate on every owner-scoped verb are compiled into the Forgejo ops surface ([`ward-kdl.forgejo.guardfile.kdl`](cmd/ward-kdl/ward-kdl.forgejo.guardfile.kdl)), and the bot attribution defaults into the embedded fleet manifest ([`ward-kdl.fleet.kdl`](cmd/ward-kdl/ward-kdl.fleet.kdl)) - none of them runtime config. The forge-agnostic verb gate runs against any repo, but the agent driver and `ward ops forgejo` **cannot be repointed at your own instance after install**: retargeting means a **source build** with those two files edited and the binary rebuilt. Turning the endpoint, token, and owner gate into operator config is tracked in [ward#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395) and [ward#396](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/396).

## What it does

Wraps a project's dev verbs behind cli-guard's policy gate. Every ward-managed repo is expected to declare the `build` / `test` / `install` triple in `.ward/ward.yaml`; many also expose `vet`, `lint`, `tidy`, and `cover`. Every invocation validates argv against a shell-metacharacter policy, writes one append-only JSONL audit row to `~/.ward/audit/<repo>.jsonl`, and gates repo verbs on a clean-and-synced tree so the row can be reconstructed from git history. See [`docs/exec-verb.md`](docs/exec-verb.md).

**Enforcement depth is platform-conditional.** On **Linux** sandboxed verbs run inside cli-guard's sandbox jail, so the gate holds at arbitrary process depth. On **macOS and Windows** - what the brew-first path predominantly serves - enforcement is **depth-0** (the harness allowlist only): a child spawned by a gated verb can invoke a wrapped tool without re-entering the gate, a known limitation by design. See [`docs/exec-verb.md`](docs/exec-verb.md) (Enforcement depth by platform).

Each repo declares its verbs (and an optional `security:` policy) in [`.ward/ward.yaml`](.ward/ward.yaml). For the field-by-field schema see [`docs/ward-yaml.md`](docs/ward-yaml.md). `ward doctor` verifies `.ward/ward.yaml` and the Makefile have not drifted: each exposed target must carry a `## <description>` help comment whose text equals the command's `description:`, and `run:` must be exactly `make <name>`. See [`docs/doctor.md`](docs/doctor.md) (Allowlist contract).

## Install

Install from the centralized flight-deck tap:

```
brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap
brew install coilyco-flight-deck/tap/ward
```

The explicit-URL form is required because the tap lives on forgejo, not github.com. The formula installs `ward` (stamped with the release tag) plus the `warded` symlink, and nothing else. The `ward-kdl` authoring binary is **not** installed - its surfaces are already embedded in `ward`. Spec authors who need `ward-kdl` build it from a ward checkout - see [ward-kdl-authoring.md](docs/ward-kdl-authoring.md). Upgrade with `ward upgrade`.

Each release ships the full `ward-{darwin,linux}-{amd64,arm64}` matrix + `SHA256SUMS`. Most install via Homebrew (above); a GitHub arrival grabs a checksummed binary ([release-binaries.md](docs/release-binaries.md)).

**Releases live on Forgejo.** This repo is canonical on [forgejo.coilysiren.me/coilyco-flight-deck/ward](https://forgejo.coilysiren.me/coilyco-flight-deck/ward); the github.com [Releases page](https://github.com/coilyco-flight-deck/ward/releases) mirrors the same tags and checksums; changelog on Forgejo.

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

Engineer runs are **detached**: the attach-and-watch `--watch` retired, so interactive work now lives on the [director](docs/agent-director.md) surface. New to the agent driver? [`docs/first-run.md`](docs/first-run.md) is the ordered path from zero to a verifiable `warded ... --print` dry run.

## When a run breaks

A `warded` run that failed or seemed to do nothing has a single symptom-indexed entry point: [`docs/troubleshooting.md`](docs/troubleshooting.md). It is indexed by **what you saw**, not by which subsystem failed - "launched then nothing happened", "never launched", "`ward exec` refused", "nothing landed on `main`" - and each row routes to the one diagnostic surface (the `~/.ward/agent-logs/<container>/` drain, a NO-GO comment on the issue, or a host auth refresh) and the fix. Start there before opening any per-subsystem doc.

## Three layers, told apart by when they run

`ward` absorbs the operator surface from the retiring [coily][coily]. The pieces are easiest to keep straight by **when** each runs:

- **[cli-guard][cli-guard]** - the **engine**. The policy-and-routing framework ward consumes (pinned via go.mod). Thin consumer, not a fork.
- **[`ward-kdl`](docs/ward-kdl.md)** - the **build-time generator**. Compiles a KDL guardfile into an audited CLI: the `ward ops <api>` REST surfaces (forgejo, aws, tailscale, ...), buildable as `ward-kdl-{read,write,admin}` tiers. Not a public install artifact - its surfaces are embedded in `ward`.
- **`ward`** - the **run-time product**. Embeds those generated surfaces and adds the `agent` + `exec` layers. Composite control flow (the `agent` roster, `git`) stays hand-written Go.

See [`docs/architecture.md`](docs/architecture.md).

## Where to go next

Over 60 pages under [`docs/`](docs/) cover each surface. The anchors:

- **The verb gate** - [exec-verb.md](docs/exec-verb.md) (the gate), [verb-fallback.md](docs/verb-fallback.md), [git-verbs.md](docs/git-verbs.md), [audit.md](docs/audit.md), [doctor.md](docs/doctor.md), [install-hooks.md](docs/install-hooks.md). A claude-only, fail-open PreToolUse **hint** hook ([hook.md](docs/hook.md)) can route bare-binary calls back through the gate on the host - a convenience, not the boundary. The boundary is the verb gate itself, plus the container edge in the agent flow ([enforcement-boundary.md](docs/enforcement-boundary.md)).
- **The agent driver** - [first-run.md](docs/first-run.md) (zero to a first `--print` dry run), [agent.md](docs/agent.md) (the reference), the roster [agent-engineer.md](docs/agent-engineer.md) / [agent-director.md](docs/agent-director.md) / [agent-advisor.md](docs/agent-advisor.md), the [agent-gate.md](docs/agent-gate.md), [agent-credentials.md](docs/agent-credentials.md), [agent-observability.md](docs/agent-observability.md).
- **The container** - [container.md](docs/container.md), [container-reap.md](docs/container-reap.md) (land-or-salvage on teardown), [container-multi-repo.md](docs/container-multi-repo.md), [container-substrate.md](docs/container-substrate.md).
- **Operator surface (ward-kdl / ops)** - [ward-kdl.md](docs/ward-kdl.md), [ward-kdl-tiers.md](docs/ward-kdl-tiers.md), [ops-forgejo.md](docs/ops-forgejo.md).
- **Build & release** - [homebrew-build.md](docs/homebrew-build.md), [release.md](docs/release.md), [github-mirror.md](docs/github-mirror.md), [golangci.md](docs/golangci.md).

## Status

v0.x, and early on purpose. ward is a single-maintainer tool in active internal use across the coilyco-flight-deck fleet, now opening up - so a small public audience (few stars, few forks) is expected for the stage, not decay. The high release count is the same: releases are automated per-merge by CI on every push to `main`, so the version is a build counter, not a maturity signal. Downstream consumers upgrade to the `ward` binary and `.ward` config on their own schedule. Minor API breaks ship in `main` with a note in the commit body, so pin a commit until v1.0.0.

## Related

- [cli-guard][cli-guard] - the underlying security-boundary framework.
- [coily][coily] - the operator-facing cli-guard consumer whose surface ward absorbs.
- [cli-mcp][cli-mcp] - a sibling cli-guard consumer that projects a urfave/cli tree as an MCP server.
- [comparison-openshell.md](docs/comparison-openshell.md) - ward vs NVIDIA OpenShell: a verb-level gate, not a kernel sandbox.
- [comparison-container-use.md](docs/comparison-container-use.md) - ward vs Dagger container-use: a capability gate and autonomous driver, not container isolation with a human at the merge.

## Support

**Canonical development happens on [Forgejo][ward-forgejo]** - `main`, the issues, and every commit live there. That instance's registration is closed, so the **GitHub mirror is the public front door for everyone except the maintainer**: file a [bug or feature request][new-issue] there with just a GitHub account and a maintainer carries an accepted change across. If you are working directly in the canonical repo, use Forgejo issues and Forgejo `closes #N` links. The full contributor flow is in [CONTRIBUTING.md](CONTRIBUTING.md). Conduct: [Code of Conduct](CODE_OF_CONDUCT.md). Security: [SECURITY.md](SECURITY.md). License: [`LICENSE`](./LICENSE).

[cli-guard]: https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard
[coily]: https://github.com/coilyco-bridge/coily
[cli-mcp]: https://github.com/coilysiren/cli-mcp
[new-issue]: https://github.com/coilyco-flight-deck/ward/issues/new/choose
[ward-forgejo]: https://forgejo.coilysiren.me/coilyco-flight-deck/ward

## See also

- [docs/README.md](docs/README.md) - the docs index: every doc grouped by subsystem.
- [docs/architecture.md](docs/architecture.md) - ward in three layers (cli-guard, ward-kdl, ward).
- [AGENTS.md](AGENTS.md) - agent-facing operating rules.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.
- [docs/ward-yaml.md](docs/ward-yaml.md) - field-by-field `.ward/ward.yaml` schema reference.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
