# ward features

Inventory of what `ward` ships today.

## Core gate

- `ward exec` - gated repo dev verbs.
- `ward audit` - append-only audit trail.
- `ward git` - audited git.
- `ward setup` - config/cache warmer.
- `ward doctor` - runtime config validation.
- `source-doc-refs` - source-comment documentation path validation.
- `.ward/ward.yaml` - repo config schema in [ward-yaml.md](ward-yaml.md).

## Agent surface

- **`ward agent`** - the guarded execution layer.
- **`warded`** - the symlinked public face.
- `WARD_CONFIG_REF` accepts local KDL bundles.
- `ward agent director queue` / `status` - read-only queue view.
- Read-only Forgejo issue-comment guard.
- Reservation and dispatch comments clean up after release.
- Harness install hooks for claude, codex, goose, and opencode.
- Core tracker and forge adapters do not depend on generated `ward ops` leaves.
- Launch-intent vs running-engineer split in list, dispatch-health, reap, and director.
- Issue-thread-backed reservations with disposable cache and `ward agent reservations clear`.
- Open-PR backpressure gate.
- Issue-scoped director dispatch.
- Dispatch broker. See [agent-dispatch-broker.md](agent-dispatch-broker.md).
- PR-workflow tools with KDL defaults. See [agent-pr-workflow.md](agent-pr-workflow.md).
- PR lifecycle close/reopen/recovery tools.
- PR repair classification.
- Ward-owned Claude tool-failure producer and local schema-v1 buffer. See [tool-failures.md](tool-failures.md).
- Director defaults read-only; autonomous drain needs `--burndown` / `--drain`.
- `ward agent` roles and workflows. See [agent.md](agent.md), [agent-roster.md](agent-roster.md), [agent-flags.md](agent-flags.md), [agent-roles.md](agent-roles.md), [agent-harnesses.md](agent-harnesses.md), [agent-lifecycle.md](agent-lifecycle.md), [agent-director.md](agent-director.md), [agent-ops.md](agent-ops.md), [agent-dispatch-health.md](agent-dispatch-health.md), [dispatch-review.md](dispatch-review.md), and [agent-workflow.md](agent-workflow.md).
- Dispatch-health surfacing.
- PR repair input mode.

## Container surface

- The ephemeral run box - see [container.md](container.md),
  [container-contract.md](container-contract.md),
  [container-lifecycle.md](container-lifecycle.md), and
  [container-substrate.md](container-substrate.md).
- Public demo image build. See [demo-image.md](demo-image.md).

## ward-kdl

- The build-time authoring layer - see [ward-kdl.md](ward-kdl.md), [ward-kdl-authoring.md](ward-kdl-authoring.md), [ward-kdl-surface.md](ward-kdl-surface.md), and [ward-kdl-in-ward.md](ward-kdl-in-ward.md).
- It embeds the shipped agent role catalog from [ward-kdl.role-definitions.kdl](../.ward/ward-kdl/ward-kdl.role-definitions.kdl).
- It accepts `first input` as exec-guard sugar for `arg0`.
- Embedded Forgejo surface includes raw Actions log fetch, runner-token mint, and PR edit leaves.
- Runtime `WARD_CONFIG_REF` bundles affect edge/operator surfaces, not the core agent control plane.
- Coilyco-targeted operator surfaces fail fast when they would otherwise fall back to the baked example bundle.

## Release and docs

- Two-stage release: promote gates main and fast-forwards `release`; release publishes the promoted sha on a queue. See [release.md](release.md).
- [compat-surface.md](compat-surface.md) - release-facing provider matrix.
- [release.md](release.md) and [release-binaries.md](release-binaries.md).
- [homebrew-build.md](homebrew-build.md), [golangci.md](golangci.md), and [troubleshooting.md](troubleshooting.md).
- [docs/README.md](README.md) - docs index.

## See also

- [../README.md](../README.md) - the front page.
- [../AGENTS.md](../AGENTS.md) - the operating rules.
- [features-release-tooling.md](features-release-tooling.md) - the hook and release convention.
- [../.ward/ward.yaml](../.ward/ward.yaml) - the repo allowlist.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
