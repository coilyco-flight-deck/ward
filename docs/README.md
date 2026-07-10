# ward docs index

Start here if you open `docs/` directly. This tree now holds the release-era
operating surface only.

## Core

- [architecture.md](architecture.md) - the three layers.
- [FEATURES.md](FEATURES.md) - what ships today.
- [exec-verb.md](exec-verb.md) - the guarded dev-verb gate.
- [audit.md](audit.md) - the append-only audit trail.
- [verb-fallback.md](verb-fallback.md) - `ward exec` fallback routing.
- [git-verbs.md](git-verbs.md) - audited git.
- [git-clone.md](git-clone.md) - destination-gated clone.
- [config-discovery.md](config-discovery.md) - how ward finds config.
- [config-source.md](config-source.md) - launch-time config sources.
- [ward-yaml.md](ward-yaml.md) - `.ward/ward.yaml`.
- [workspace.md](workspace.md) - local source checkout mode.
- [homebrew-build.md](homebrew-build.md) - build and release packaging.
- [error-reporting.md](error-reporting.md) - panic telemetry.
- [golangci.md](golangci.md) - lint policy.
- [release.md](release.md) - release pipeline.
- [release-binaries.md](release-binaries.md) - tagged binaries and checksums.
- [forge-linking.md](forge-linking.md) - link targets by forge.
- [troubleshooting.md](troubleshooting.md) - start here when a run fails.

## How to use this index

- if you want the repo gate, start in Core.
- if you want the agent workflow, start in Agent.
- if you want container behavior, start in Container.
- if you want generator behavior, start in ward-kdl.

The index is intentionally smaller than the old tree. It points at durable
guides, not issue slices.

## Agent

- [first-run.md](first-run.md) - first dry run.
- [agent.md](agent.md) - the entrypoint.
- [agent-roster.md](agent-roster.md) - the generated role roster.
- [agent-roles.md](agent-roles.md) - engineer, director, advisor, qa.
- [agent-harnesses.md](agent-harnesses.md) - claude, codex, goose, opencode.
- [agent-lifecycle.md](agent-lifecycle.md) - launch, preflight, reservation.
- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [agent-workflow.md](agent-workflow.md) - landing policy and review.

## Container

- [container.md](container.md) - the ephemeral run box.
- [container-contract.md](container-contract.md) - mounts, env, permissions.
- [container-lifecycle.md](container-lifecycle.md) - launch, debug, teardown.
- [container-substrate.md](container-substrate.md) - `/substrate` and multi-repo.

## ward-kdl

- [ward-kdl.md](ward-kdl.md) - the build-time authoring layer.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the generated surface overview.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - exec mounts into `ward`.
- [ward-kdl-authoring.md](ward-kdl-authoring.md) - author and rebuild guardfiles.
- [ward-docker-exec.md](ward-docker-exec.md) - `ward docker exec`.

## Ops and examples

- [ops-forgejo.md](ops-forgejo.md) - Forgejo ops in ward.
- [example-repo.md](example-repo.md) - the minimal managed repo.
- [demo.md](demo.md) - the runnable demo.

## Old pages that disappeared

The old flat tree had separate issue pages for harnesses, reaping, logs,
container plumbing, and generated ward-kdl references. Those are now folded
into the shorter guides above.

## See also

- [../README.md](../README.md) - the front page.
- [../AGENTS.md](../AGENTS.md) - operating rules.
- [../.ward/ward.yaml](../.ward/ward.yaml) - the allowlist.
