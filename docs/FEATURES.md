---
doc_goal: Keep the inventory of shipped surfaces current after the docs collapse, without dragging the deleted issue-slice pages along with it.
---
# ward features

Inventory of what `ward` ships today.

## Core gate

- `ward exec` - gated repo dev verbs.
- `ward audit` - the append-only audit trail.
- `ward git` - audited git.
- `ward setup` - the live config/cache warmer.
- `ward doctor` - strict runtime config validation.
- `.ward/ward.yaml` - the repo config schema in [ward-yaml.md](ward-yaml.md).

## Agent surface

- **`ward agent`** - the guarded execution layer.
- **`warded`** - the symlinked public face.
- Core tracker/forge adapters - issue lookup, reservation, dispatch broker
  writes, reaper comments, and director merge reads do not depend on generated
  `ward ops` leaves.
- **`ward agent` roles and workflows** - see [agent.md](agent.md),
  [agent-roster.md](agent-roster.md), [agent-roles.md](agent-roles.md), [agent-harnesses.md](agent-harnesses.md),
  [agent-lifecycle.md](agent-lifecycle.md), [agent-director.md](agent-director.md),
  [agent-ops.md](agent-ops.md), [dispatch-review.md](dispatch-review.md), and
  [agent-workflow.md](agent-workflow.md). The roster resolves from effective
  role definitions plus fleet overlays, not a hand-edited role list.

## Container surface

- The ephemeral run box - see [container.md](container.md),
  [container-contract.md](container-contract.md),
  [container-lifecycle.md](container-lifecycle.md), and
  [container-substrate.md](container-substrate.md).

## ward-kdl

- The build-time authoring layer - see [ward-kdl.md](ward-kdl.md),
  [ward-kdl-authoring.md](ward-kdl-authoring.md),
  [ward-kdl-surface.md](ward-kdl-surface.md),
  [ward-kdl-in-ward.md](ward-kdl-in-ward.md), and
  [ward-docker-exec.md](ward-docker-exec.md). It also embeds the shipped agent
  role catalog from [ward-kdl.roles.kdl](../.ward/ward-kdl/ward-kdl.roles.kdl).
  Per-area guardfile refs are generated output, not release-era docs.
- Runtime `WARD_CONFIG_REF` bundles affect edge/operator surfaces, not the core
  agent control plane.
- Coilyco-targeted operator surfaces fail fast when they would otherwise fall
  back to the baked example bundle, naming the active source and the expected
  `WARD_CONFIG_REF` bundle.

## Release and docs

- [release.md](release.md) and [release-binaries.md](release-binaries.md).
- [homebrew-build.md](homebrew-build.md), [golangci.md](golangci.md), and
  [troubleshooting.md](troubleshooting.md).
- [docs/README.md](README.md) - the docs index.

## What changed

The inventory is now grouped by durable surface instead of mirroring the old
issue-slice docs. That makes the file list smaller and keeps the release-era
contract in one place per subsystem.

## See also

- [../README.md](../README.md) - the front page.
- [../AGENTS.md](../AGENTS.md) - the operating rules.
- [../.ward/ward.yaml](../.ward/ward.yaml) - the repo allowlist.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
