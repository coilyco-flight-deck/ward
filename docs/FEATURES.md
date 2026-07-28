# ward features

Inventory of what `ward` ships today.

## Core gate

- `ward exec` - gated repo dev verbs.
- `ward audit` - append-only audit trail.
- `ward git` - audited git.
- `ward config drop` - clear local `config-ref`.
- `ward setup` - first-run local config bootstrap + policy check.
- `ward doctor` - runtime config validation.
- `source-doc-refs` - source-comment documentation path validation.
- `.ward/ward.yaml` - repo config schema in [ward-yaml.md](ward-yaml.md).

## Agent surface

- **`ward agent`** - the guarded execution layer.
- **`warded`** - the symlinked public face.
* Config: env or `~/.ward/config.yaml`.
- `ward agent director queue` / `status` - read-only queue view.
- Read-only Forgejo issue-comment guard.
- Reservation and dispatch comments clean up after release.
- Harness install hooks for claude, codex, goose, and opencode.
- Provider-neutral, read-only context-bundle handoff with strict role and agent
  binding, selected home projection, optional appended tools, and no authority
  fields. See [context-bundle.md](context-bundle.md).
- Core tracker and forge adapters do not depend on AOSguard or generated operator leaves.
- Launch-intent vs running-engineer split in list, dispatch-health, reap, and director.
- Issue-thread-backed reservations with disposable cache and `ward agent reservations clear`.
- Open-PR backpressure gate.
- Issue-scoped director dispatch.
- Independently supervised Compose dispatch broker with durable request IDs,
  restart reconciliation, and sibling engineer/QA launch isolation. See
  [agent-dispatch-broker.md](agent-dispatch-broker.md).
- PR-workflow tools with KDL defaults. See [agent-pr-workflow.md](agent-pr-workflow.md).
- PR lifecycle close/reopen/recovery tools.
- PR repair classification.
- Ward-owned Claude tool-failure producer and local schema-v1 buffer. See [tool-failures.md](tool-failures.md).
- Director defaults read-only; autonomous drain needs `--burndown` / `--drain`.
- `ward agent issue create` files a Forgejo issue through the read-only
  director credential broker without dispatching an engineer.
- `ward agent` roles and workflows. See [agent.md](agent.md).
- Dispatch-health surfacing.
- PR repair input mode.

## Container surface

- The ephemeral run box - see [container.md](container.md),
  [container-contract.md](container-contract.md),
  [container-lifecycle.md](container-lifecycle.md), and
  [container-substrate.md](container-substrate.md).
- Claude in Chrome browser computer-use is disabled for Claude Code containers.
- Optional context bundles stay authority-free. Ward retains credentials,
  permissions, mounts, network, and launch authority.
- Public demo image build. See [demo-image.md](demo-image.md).

## AOS policy and AOSguard boundary

- Ward directly embeds its native agent role catalog and launch policy from [roles.kdl](../.ward/roles.kdl).
- AOSguard owns generated operator APIs in AOS. Ward does not ship generated operator leaves.
- A stale or unavailable AOSguard config reference cannot affect Ward's native `agent`, `container`, `exec`, help, or version paths.

## Release and docs

- Two-stage release ([ward#1117](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1117)): promote.yml gates every main push and
  fast-forwards `release` when green; release.yml runs on `release` pushes
  under a no-cancel concurrency queue. See [release.md](release.md).
- [compat-surface.md](compat-surface.md) - the release-facing provider matrix.
- [release.md](release.md) and [release-binaries.md](release-binaries.md).
- [homebrew-build.md](homebrew-build.md), [golangci.md](golangci.md), and [troubleshooting.md](troubleshooting.md).
- [docs/README.md](README.md) - docs index.

## See also

- [../README.md](../README.md) - front page
- [../AGENTS.md](../AGENTS.md) - operating rules.
- [features-release-tooling.md](features-release-tooling.md) - release tooling.
- [../.ward/ward.yaml](../.ward/ward.yaml) - repo allowlist.
