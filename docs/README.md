---
doc_goal: Route every supported Ward contract from one small, complete documentation index.
---
# Ward documentation

Start with [first-run.md](first-run.md). Use [troubleshooting.md](troubleshooting.md)
when a command or run does not behave as expected.

## Product and repository commands

* [FEATURES.md](FEATURES.md) - shipped capability inventory.
* [architecture.md](architecture.md) - product and authority boundaries.
* [terminology.md](terminology.md) - canonical operational vocabulary.
* [compat-surface.md](compat-surface.md) - provider compatibility matrix.
* [exec-verb.md](exec-verb.md) - governed repository commands and fallback.
* [git-verbs.md](git-verbs.md) - governed Git commands, including clone.
* [audit.md](audit.md) - append-only invocation records.
* [config-source.md](config-source.md) - configuration discovery and precedence.
* [ward-yaml.md](ward-yaml.md) - repository configuration schema.
* [doctor.md](doctor.md) - configuration validation and remedies.
* [workspace.md](workspace.md) and [windows-development.md](windows-development.md) - contributor builds.
* [release.md](release.md) - maintainer release and packaging contract.
* [forge-linking.md](forge-linking.md) - canonical and contributor-facing links.

## Agent execution

* [agent.md](agent.md) - `ward agent` entry point.
* [agent-roster.md](agent-roster.md) and [agent-flags.md](agent-flags.md) - generated command references.
* [agent-roles.md](agent-roles.md) - fixed workflow roles and their limits.
* [agent-harnesses.md](agent-harnesses.md) - supported harness adapters and credentials.
* [agent-lifecycle.md](agent-lifecycle.md) - launch checks and workflow handoff.
* [agent-director.md](agent-director.md) - attached read-only supervision.
* [agent-ops.md](agent-ops.md) - list, logs, stop, reap, and dispatch status.
* [agent-dispatch-health.md](agent-dispatch-health.md) - health summaries and consumers.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - broker authority and durable requests.
* [agent-dispatch-health.md](agent-dispatch-health.md) - restart and retained-state recovery.
* [agent-reservation.md](agent-reservation.md) - issue-thread reservations and cache cleanup.
* [agent-workflow.md](agent-workflow.md) - landing workflows and review gate.
* [agent-pr-workflow.md](agent-pr-workflow.md) - pull-request status, recovery, and mutation verbs.
* [agent-observability.md](agent-observability.md) - secret-safe artifacts and schemas.
* [agent-clusters.md](agent-clusters.md) and [agent-peer-collaboration.md](agent-peer-collaboration.md) - repository-free collaboration.
* [agent-release.md](agent-release.md) and [agent-release-transaction.md](agent-release-transaction.md) - typed release handoff and deploy-state transaction.
* [context-bundle.md](context-bundle.md) - authority-free context projection.

## Container execution

* [container.md](container.md) - ephemeral execution overview.
* [container-contract.md](container-contract.md) - mounts, environment, credentials, and skills.
* [container-lifecycle.md](container-lifecycle.md) - launch, drain, rescue, reap, and cleanup.
* [container-staging.md](container-staging.md) - host staging placement and security.
* [container.md](container.md) - workspace, references, and multi-repo layout.

## See also

* [project README](../README.md) - install and quick usage.
* [agent rules](../AGENTS.md) - mandatory contributor doctrine.
* [repository config](../.ward/ward.yaml) - this repo's governed commands.
