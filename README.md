# ward

Ward is the governed execution layer for unattended coding agents. It keeps work in fresh clones and least-access containers, then carries it through issue, branch, PR, logs, and merge outcomes with a durable audit trail.

## Why ward

Use Ward when the work breaks into separable streams and you want the system to keep moving even if one stream stalls.

- concurrency for independent work streams.
- resumability across interruptions and long-running work.
- failure containment through fresh clones and isolated engineer containers.
- auditability through issues, branches, PRs, logs, outcomes, and review trail.
- role separation between director, engineer, QA, and review lanes.
- reproducibility from declared context and containerized runs.
- backlog throughput across separable tasks.
- human interruptibility by issue and PR without losing one giant in-flight context.

Ward is the better fit for parallel, separable, auditable, failure-prone work. A single strong goal agent can still be faster for one coherent refactor through a tight core subsystem.

If the orchestration itself is flaky, that is a Ward product bug, not an operator burden. The target model is still the one above.

`ward agent` launches an authenticated coding CLI (claude, codex, goose, ...) into an ephemeral, least-access container and drives it through an issue-to-merge workflow or an issue-to-PR workflow, while bounded by credential scoping and a durable audit trail. Functionally it is a manifest-backed harness driver - it knows how to launch each agent through its own CLI dialect - but the external product is the governed execution layer around it, not the driver. That surface is exposed as **`warded`**, a thin symlink onto `ward agent`, and sits on the three-layer split covered below and in [`docs/architecture.md`](docs/architecture.md).

## Who it's for

- **A contributor (human or agent)** who wants every `build` / `test` / `lint` run argv-validated, audited, and gated on a clean tree - one wrapper instead of bare `make` / `go` / `gh` / `aws`. Forge-agnostic: point it at any repo.
- **An operator** running an autonomous agent fleet who wants each run boxed in a throwaway container, its reach bounded by an allowlist, and its whole session recorded - not a trusted shell.

## What it requires

- **macOS or Linux + Homebrew** to install the binary (see [Install](#install)).
- **A Forgejo instance** for ward's own operator surface (`ward ops forgejo`). ward is **Forgejo-canonical** for ward itself: it carries Forgejo issues and pushes to a Forgejo `main`, and the GitHub mirror is read-only and PR-gated. The agent driver follows the target repo's authority policy, so `coilysiren/*` can be GitHub-authoritative. Which Forgejo, exactly? See the note below the list.
- **Docker** for the container agent flow - each `warded` run boots an ephemeral container, configures forge git auth inside it, runs the agent, and reaps it. The first run pulls one image, `forgejo.coilysiren.me/coilyco-flight-deck/agentic-os-full:latest` (anonymous pull, no login). See [`docs/container.md`](docs/container.md) for the registry, tag policy, and how to pin off the moving tag.

The plain verb gate (`ward exec`, `ward git`, `ward audit`) needs none of the above - just the repo and its `.ward/ward.yaml`.

**Which Forgejo?** As shipped, ward defaults its guarded Forgejo surfaces to `forgejo.coilysiren.me` and `coily*`-owned orgs. The endpoint, token path, and owner gate are read from the selected config bundle for those edge surfaces ([`docs/config-source.md`](docs/config-source.md)), so `WARD_CONFIG_REF` can retarget them without a source rebuild. Rebuilding from source only changes the baked default bundle and embedded defaults. The forge-agnostic verb gate still runs against any repo.

When a doc or example needs a concrete GitHub repo that should actually resolve,
use `coilysiren/example` or `https://github.com/coilysiren/example`. It is a
public placeholder target, not a deployment prerequisite.

## What it does

Wraps a project's dev verbs behind cli-guard's policy gate. Every ward-managed repo is expected to declare the `build` / `test` / `install` triple in `.ward/ward.yaml`; many also expose `vet`, `lint`, `tidy`, and `cover`. Every invocation validates argv against a shell-metacharacter policy, writes one append-only JSONL audit row to `~/.ward/audit/<repo>.jsonl`, and gates repo verbs on a clean-and-synced tree so the row can be reconstructed from git history. See [`docs/exec-verb.md`](docs/exec-verb.md).

**Enforcement depth is platform-conditional.** On **Linux** sandboxed verbs run inside cli-guard's sandbox jail, so the gate holds at arbitrary process depth. On **macOS and Windows** - what the brew-first path predominantly serves - enforcement is **depth-0** (the harness allowlist only): a child spawned by a gated verb can invoke a wrapped tool without re-entering the gate, a known limitation by design. See [`docs/exec-verb.md`](docs/exec-verb.md) (Enforcement depth by platform).

Each repo declares its verbs (and an optional `security:` policy) in [`.ward/ward.yaml`](.ward/ward.yaml). For the field-by-field schema see [`docs/ward-yaml.md`](docs/ward-yaml.md). `ward setup` pre-bakes the selected runtime source, and [`ward doctor`](docs/doctor.md) validates that the selected runtime config is operational.

## Install

Install from the centralized flight-deck tap:

```
brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap
brew install coilyco-flight-deck/tap/ward
```

The explicit-URL form is required because the tap lives on forgejo, not github.com. The formula installs `ward` (stamped with the release tag) plus the `warded` symlink, and nothing else. The `ward-kdl` authoring binary is **not** installed - its surfaces are already embedded in `ward`. Most adopters stop at `.ward/ward.yaml`; spec authors who need `ward-kdl` build it from a ward checkout - see [ward-kdl-authoring.md](docs/ward-kdl-authoring.md).

**Building from source.** ward's `go.mod` pins [cli-guard][cli-guard] from `forgejo.coilysiren.me/coilyco-flight-deck/cli-guard`, so a plain `go build` needs that Forgejo host reachable and the repo public. For local development, `make workspace` resolves cli-guard from a sibling checkout through a gitignored `go.work`; see [docs/workspace.md](docs/workspace.md).

Each release ships the full `ward-{darwin,linux}-{amd64,arm64}` matrix + `SHA256SUMS`. Most install via Homebrew (above); a GitHub arrival grabs a checksummed binary ([release-binaries.md](docs/release-binaries.md)).

**Releases live on Forgejo.** This repo is canonical on [forgejo.coilysiren.me/coilyco-flight-deck/ward](https://forgejo.coilysiren.me/coilyco-flight-deck/ward); the github.com [Releases page](https://github.com/coilyco-flight-deck/ward/releases) mirrors the same tags and checksums; changelog on Forgejo.

## Usage

The audited verb gate, on any repo:

```
ward exec build          # run a declared dev verb through the gate
ward exec test
ward git commit -m ...   # concurrency-safe, audited git
ward audit tail --follow # stream the audit log
ward setup               # warm and validate the selected config source
ward doctor              # validate the selected config source
```

The agent driver, against the repo's authoritative issue thread. `warded` is a thin symlink onto `ward agent` - read it as a protective circle, the container bounding the agent's reach, not "warded off":

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

`ward` absorbs the operator surface from the retiring ops CLI. The pieces are easiest to keep straight by **when** each runs:

- **[cli-guard][cli-guard]** - the **engine**. The policy-and-routing framework ward consumes (pinned via go.mod). Thin consumer, not a fork.
- **[`ward-kdl`](docs/ward-kdl.md)** - the **build-time generator**. Compiles a KDL guardfile into an audited CLI: the `ward ops <api>` REST surfaces (forgejo, aws, tailscale, ...), buildable as `ward-kdl-{read,write,admin}` tiers. Not a public install artifact - its surfaces are embedded in `ward`.
- **`ward`** - the **run-time product**. Embeds those generated surfaces and adds the `agent` + `exec` layers. Composite control flow (the `agent` roster, `git`) stays hand-written Go.

See [`docs/architecture.md`](docs/architecture.md).

## Where to go next

Over 60 pages under [`docs/`](docs/) cover each surface. The anchors:

- **The verb gate** - [exec-verb.md](docs/exec-verb.md) (the gate), [verb-fallback.md](docs/verb-fallback.md), [git-verbs.md](docs/git-verbs.md), [audit.md](docs/audit.md), [doctor.md](docs/doctor.md). The boundary is the verb gate itself, plus the container edge in the agent flow ([container-contract.md](docs/container-contract.md)).
- **The agent driver** - [first-run.md](docs/first-run.md) (zero to a first `--print` dry run), [agent.md](docs/agent.md) (the reference), the roster [agent-engineer.md](docs/agent-engineer.md) / [agent-director.md](docs/agent-director.md) / [agent-advisor.md](docs/agent-advisor.md), [agent-lifecycle.md](docs/agent-lifecycle.md), [agent-ops.md](docs/agent-ops.md).
- **The container** - [container.md](docs/container.md), [container-lifecycle.md](docs/container-lifecycle.md) (land-or-salvage on teardown), [container-substrate.md](docs/container-substrate.md).
- **The demo image** - [docs/demo-image.md](docs/demo-image.md).
- **Operator surface (ward-kdl / ops)** - [ward-kdl.md](docs/ward-kdl.md), [ward-kdl-surface.md](docs/ward-kdl-surface.md), [ops-forgejo.md](docs/ops-forgejo.md).
- **Build & release** - [homebrew-build.md](docs/homebrew-build.md), [release.md](docs/release.md), [golangci.md](docs/golangci.md).

## Status

v0.x, and early on purpose. ward is a single-maintainer tool in active internal use across the coilyco-flight-deck fleet, now opening up - so a small public audience (few stars, few forks) is expected for the stage, not decay. The high release count is the same: releases are automated per-merge by CI on every push to `main`, so the version is a build counter, not a maturity signal. Downstream consumers upgrade to the `ward` binary and `.ward` config on their own schedule. Minor API breaks ship in `main` with a note in the commit body, so pin a commit until v1.0.0.

## Related

- [cli-guard][cli-guard] - the underlying security-boundary framework.
- [compat-surface.md](docs/compat-surface.md) - the release-facing matrix of shipped providers, operator-local sources, and explicit non-providers.

## Support

**Canonical development happens on [Forgejo][ward-forgejo]** - `main`, the issues, and every commit live there. That instance's registration is closed, so the **GitHub mirror is the public front door for everyone except the maintainer**: file a [new issue][new-issue] there with just a GitHub account and a maintainer carries an accepted change across. If you are working directly in the canonical repo, use Forgejo issues and Forgejo `closes #N` links. For `coilysiren/*`, the agent driver resolves the authoritative tracker from the repo-authority policy instead of assuming Forgejo. Conduct: [Code of Conduct](CODE_OF_CONDUCT.md). Security: [SECURITY.md]. License: [`LICENSE`](./LICENSE).

[cli-guard]: https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard
[new-issue]: https://github.com/coilyco-flight-deck/ward/issues/new
[ward-forgejo]: https://forgejo.coilysiren.me/coilyco-flight-deck/ward

## See also

- [docs/README.md](docs/README.md) - the docs index: every doc grouped by subsystem.
- [docs/architecture.md](docs/architecture.md) - ward in three layers (cli-guard, ward-kdl, ward).
- [AGENTS.md](AGENTS.md) - agent-facing operating rules.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [docs/features-release-tooling.md](docs/features-release-tooling.md) - cross-repo tooling and release convention.
- [docs/compat-surface.md](docs/compat-surface.md) - release-facing provider compatibility matrix.
- [docs/doctor.md](docs/doctor.md) - runtime config validation.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.
- [docs/ward-yaml.md](docs/ward-yaml.md) - field-by-field `.ward/ward.yaml` schema reference.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
