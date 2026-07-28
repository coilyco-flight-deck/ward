# ward docs index

Start here if you open `docs/` directly. The tree holds the release-era operating surface.

## Core

- [architecture.md](architecture.md) - three layers.
- [terminology.md](terminology.md) - vocabulary, conceptual model, non-equivalences, and analogy bank.
- [FEATURES.md](FEATURES.md) - shipped surface.
- [compat-surface.md](compat-surface.md) - shipped providers and non-providers.
- [exec-verb.md](exec-verb.md) - guarded dev verbs.
- [audit.md](audit.md) - append-only audit trail.
- [verb-fallback.md](verb-fallback.md) - fallback routing.
- [doctor.md](doctor.md) - runtime config validation.
- [git-verbs.md](git-verbs.md) - audited git.
- [git-clone.md](git-clone.md) - destination-gated clone.
- [config-discovery.md](config-discovery.md) - config lookup.
- [config-source.md](config-source.md) - launch-time config sources.
- [ward-yaml.md](ward-yaml.md) - `.ward/ward.yaml`.
- [workspace.md](workspace.md) - local checkout mode.
- [homebrew-build.md](homebrew-build.md) - build and release packaging.
- [error-reporting.md](error-reporting.md) - panic telemetry.
- [tool-failures.md](tool-failures.md) - Claude tool-failure producer.
- [golangci.md](golangci.md) - lint policy.
- [release.md](release.md) - release pipeline.
- [release-binaries.md](release-binaries.md) - tagged binaries and checksums.
- [promote-run-2491.md](promote-run-2491.md) - promote refspec checkpoint.
- [release-run-2495.md](release-run-2495.md) - release checkout checkpoint.
- [release-run-2497.md](release-run-2497.md) - follow-up release checkout checkpoint.
- [release-run-2501.md](release-run-2501.md) - release stable asset tag checkpoint.
- [forge-linking.md](forge-linking.md) - forge-specific link targets.
- [troubleshooting.md](troubleshooting.md) - start here on failure.

## Agent

- [first-run.md](first-run.md) - first dry run.
- [agent.md](agent.md) - entrypoint.
- [agent-roster.md](agent-roster.md) - generated roster.
- [agent-flags.md](agent-flags.md) - generated flag tree.
- [agent-roles.md](agent-roles.md) - engineer, director, qa.
- [agent-harnesses.md](agent-harnesses.md) - claude, codex, goose, opencode.
- [context-bundle.md](context-bundle.md) - provider-neutral context and tool handoff.
- [agent-lifecycle.md](agent-lifecycle.md) - launch, preflight, reservation.
- [agent-check-placement.md](agent-check-placement.md) - broker/pre-flight guard matrix.
- [agent-director.md](agent-director.md) - read-only director lane.
- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [agent-reservation-cache.md](agent-reservation-cache.md) - cache-only reservation cleanup.
- [agent-dispatch-health.md](agent-dispatch-health.md) - status and alert lines.
- [agent-dispatch-broker.md](agent-dispatch-broker.md) - brokered launch contract.
- [agent-pr-workflow.md](agent-pr-workflow.md) - merge, status, wait, logs, runs, rerun.
- [agent-pr-status-object.md](agent-pr-status-object.md) - PR/CI status object, wait primitive, log hooks, examples.
- [agent-workflow.md](agent-workflow.md) - landing policy and review.
- [warded-kernel-boundary.md](warded-kernel-boundary.md) - kernel versus edge extraction boundary.

## Container

- [container.md](container.md) - ephemeral run box.
- [container-contract.md](container-contract.md) - mounts, env, permissions.
- [container-lifecycle.md](container-lifecycle.md) - launch, debug, teardown.
- [container-substrate.md](container-substrate.md) - `/substrate` and multi-repo.
- [demo-image.md](demo-image.md) - public demo image build.

## AOSguard boundary

- [aosguard-boundary.md](aosguard-boundary.md) - Ward to AOSguard ownership boundary.

## Examples

- [example-repo.md](example-repo.md) - minimal managed repo.
- [demo.md](demo.md) - runnable demo.

## See also

- [../README.md](../README.md) - the front page.
- [../AGENTS.md](../AGENTS.md) - operating rules.
- [../.ward/ward.yaml](../.ward/ward.yaml) - the allowlist.
