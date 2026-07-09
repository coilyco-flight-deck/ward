---
doc_goal: Be the landing page for docs/ - the "start here" the README/AGENTS/FEATURES trifecta already assumes exists. Group every docs/*.md by subsystem with a one-line description so a reader who opens docs/ (web UI or a fresh clone) is oriented across the whole set instead of facing a bare alphabetical file list.
---
# ward docs index

**Start here.** `docs/` holds the durable detail behind [`../README.md`](../README.md),
[`../AGENTS.md`](../AGENTS.md), and [`FEATURES.md`](FEATURES.md). This page groups every
doc by subsystem so you can find the one you want without scanning a flat file list.
New to ward? Read [architecture.md](architecture.md) first - it frames the whole system.

## Core & orientation

- [architecture.md](architecture.md) - ward in three layers (cli-guard the engine, ward-kdl the build-time generator, ward the run-time product).
- [FEATURES.md](FEATURES.md) - inventory of what ships today.
- [comparison-openshell.md](comparison-openshell.md) - ward vs NVIDIA OpenShell: verb-level gate vs kernel sandbox.
- [comparison-container-use.md](comparison-container-use.md) - ward vs Dagger container-use: capability gate + autonomous driver vs container isolation + human-at-the-merge.
- [ward-setup-doctor-inventory.md](ward-setup-doctor-inventory.md) - the paused `setup` and `doctor` behavior inventory, with the rebirth note.
- [troubleshooting.md](troubleshooting.md) - symptom-indexed entry point for a failed `warded` run.
- [audit.md](audit.md) - the append-only JSONL audit row written per invocation.
- [config-discovery.md](config-discovery.md) - how ward resolves the allowlist config path.
- [ward-yaml.md](ward-yaml.md) - field-by-field `.ward/ward.yaml` schema reference (commands + the security: block).
- [golangci.md](golangci.md) - the strict-ish golangci-lint configuration.
- [homebrew-build.md](homebrew-build.md) - Homebrew build + cli-guard pinning notes.
- [error-reporting.md](error-reporting.md) - ward's own off-by-default GlitchTip crash reporting, for top-level Go panics only.

## Contributor dev-verb gate

- [exec-verb.md](exec-verb.md) - `ward exec <verb>`: run a repo dev verb through the gate.
- [gate-demo.md](gate-demo.md) - what the gate refuses: the clean-tree + argv-metacharacter denial demo.
- [demo.md](demo.md) - the launch demo: one happy path plus two danger classes, driven live against `examples/toy/` by [`../examples/demo.sh`](../examples/demo.sh).
- [workflow-mirror.md](workflow-mirror.md) - the Forgejo/GitHub `test` workflow mirror and drift checker.
- [verb-fallback.md](verb-fallback.md) - unknown-verb rewrite to `ward exec` + the build/test/install triple.
- [git-verbs.md](git-verbs.md) - `ward git`: audited, concurrency-safe git surface.
- [git-clone.md](git-clone.md) - `ward git clone`, destination-gated.

## `ward agent` (headless harness runner)

- [first-run.md](first-run.md) - zero to a verifiable first `warded` dry run: prerequisites, install/verify, a safe `--print` first command.
- [agent.md](agent.md) - the entrypoint to the ephemeral container that carries a feature.
- [agent-subcommands.md](agent-subcommands.md) - how the roles differ (what they do, attachment, scope).
- [agent-roster.md](agent-roster.md) - the generated role roster.
- [agent-reap.md](agent-reap.md) - `ward agent reap`, the host-side idle-killer for wedged engineer containers.
- [agent-stop.md](agent-stop.md) - `ward agent stop`, the director-surface on-demand engineer stop through the dispatch broker.
- [agent-logs.md](agent-logs.md) - `ward agent logs`, the director-surface on-demand engineer log read through the dispatch broker.
- [agent-engineer.md](agent-engineer.md) - the implement-a-ticket role.
- [agent-director.md](agent-director.md) - the autonomous-backlog heartbeat role.
- [agent-director-dispatch.md](agent-director-dispatch.md) - how the director parks vs. defers a dispatch error.
- [agent-advisor.md](agent-advisor.md) - the counsel role that answers without writing code.
- [agent-advisor-fanout.md](agent-advisor-fanout.md) - advisor ref mode: structured emit + cross-repo fan-out.
- [agent-flags.md](agent-flags.md) - launch flags for the `engineer` role.
- [dispatch-review.md](dispatch-review.md) - the in-container code-review gate that runs before a diff lands ([ward#134](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/134)).
- [agent-frontload.md](agent-frontload.md) - front-loading subsystem context before detach.
- [agent-preflight.md](agent-preflight.md) - the headless pre-flight before a fire-and-forget run.
- [agent-preflight-trust.md](agent-preflight-trust.md) - the cloud-harness-only trust gate on the host read ([ward#162](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/162)).
- [agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire pre-flight guard.
- [agent-reservation.md](agent-reservation.md) - reservation + stale-host-binary checks.
- [agent-reserved-immutable.md](agent-reserved-immutable.md) - a reserved seed is immutable.
- [agent-gate.md](agent-gate.md) - the interactive pre-launch gate.
- [agent-surface.md](agent-surface.md) - the director's read-only scope-and-dispatch surface.
- [agent-surface-log-read.md](agent-surface-log-read.md) - the surface's read-only agent-log drain mount.
- [agent-github.md](agent-github.md) - GitHub as a first-class forge: token setup + the PR-landing loop.
- [github-rate-limits.md](github-rate-limits.md) - ward's GitHub client stays on the REST budget, off GraphQL.
- [director-startup-triage.md](director-startup-triage.md) - director startup triage (autonomous drain).
- [director-on-demand-surface.md](director-on-demand-surface.md) - the director's on-demand surface.
- [broker.md](broker.md) - the root credential broker that hardens the director's surface.
- [forgejo-token-audit.md](forgejo-token-audit.md) - the audited set of raw Forgejo-token read sites + the build-time guard that freezes it.
- [agent-credentials.md](agent-credentials.md) - how each harness's host credential is seeded.
- [agent-aws-creds.md](agent-aws-creds.md) - how the aws capability delivers AWS creds (export-and-inject, mount fallback).
- [agent-attribution.md](agent-attribution.md) - agent attribution on Forgejo write bodies.
- [agent-observability.md](agent-observability.md) - agent-run log drain + redacted local archive, with SigNoz deferred.
- [agent-dispatch-contract.md](agent-dispatch-contract.md) - dispatch exit codes + `meta.json` outcome enum for supervising runs.
- [agent-host-net.md](agent-host-net.md) - `--tailnet`, the opt-in network escalation.
- [agent-ts-sidecar.md](agent-ts-sidecar.md) - the Docker Desktop tailnet-route sidecar.
- [agent-tailnet-topology.md](agent-tailnet-topology.md) - repoint the network/proxy/tower via `WARD_*` env.
- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the per-agent divergence manifest.
- [agentsapi.md](agentsapi.md) - `internal/agentsapi`, the agent-agnostic seam contract.
- [agents-list.md](agents-list.md) - `ward agents list`, the fleet-roster read surface.

## Agent harnesses (drivers)

- [agent-drivers.md](agent-drivers.md) - the four harnesses (`--harness`) compared (first-run facts side by side).
- [enforcement-boundary.md](enforcement-boundary.md) - where the enforcement boundary sits per harness (container-edge verb gate).
- [agent-local-harnesses.md](agent-local-harnesses.md) - index of the local harness pages.
- [agent-local-model.md](agent-local-model.md) - bring your own Ollama: defaults, the supported route, and the current limitation ([#395](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/395)).
- [agent-claude.md](agent-claude.md) - the `claude` full cloud harness.
- [agent-codex.md](agent-codex.md) - the `codex` open-sandbox cloud harness.
- [agent-opencode.md](agent-opencode.md) - the `opencode` local Ollama-backed harness.
- [agent-qwen.md](agent-qwen.md) - the deprecated `qwen` alias, a one-line pointer to `opencode`.
- [agent-goose.md](agent-goose.md) - the `goose` local harness.

## Container subsystem

- [container.md](container.md) - the ephemeral, least-access dev container.
- [container-api.md](container-api.md) - the host<->entrypoint contract overview: bind mounts + the produced file layout.
- [container-env.md](container-env.md) - the full `WARD_*` environment contract the entrypoint reads.
- [container-capability-ladder.md](container-capability-ladder.md) - the progressive-capability ladder (`WARD_CONTEXT_LEVEL`, by driver).
- [container-permissions.md](container-permissions.md) - what the container itself may do.
- [container-substrate.md](container-substrate.md) - the read-only `/substrate` reference repos.
- [container-skill-surface.md](container-skill-surface.md) - the substrate-vs-full-tree design: why the container reads substrate skills as docs instead of rebuilding the host symlink forest, and the `WARD_CONTAINER` fence for host-only fleet scripts.
- [substrate-catalog.md](substrate-catalog.md) - the Forgejo-derived repo catalog baked into the seed as read-these-first backing data.
- [context-probe.md](context-probe.md) - design: the role-aware three-tier context probe + per-driver context-management spec.
- [container-multi-repo.md](container-multi-repo.md) - multi-repo runs (`--repo`).
- [container-precommit.md](container-precommit.md) - fresh-clone pre-commit parity.
- [container-reap.md](container-reap.md) - `ward container reap`, the teardown backstop.
- [container-lifecycle-logs.md](container-lifecycle-logs.md) - the stable lifecycle-log conventions (surfaces, line grammar, correlation ids).
- [container-lifecycle-debug.md](container-lifecycle-debug.md) - the debug-a-headless-run-from-logs-only sequence built on those conventions.
- [container-cleanup.md](container-cleanup.md) - cleaning up stopped containers.
- [container-stop.md](container-stop.md) - halting a running container.

## ward-kdl & config (build-time authoring layer)

- [ward-kdl.md](ward-kdl.md) - what ward-kdl is: the build-time authoring layer.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface, area by area.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - exec guardfiles auto-mounted into `ward`.
- [ward-docker-exec.md](ward-docker-exec.md) - `ward docker exec`, the ward=true-gated shell-into-a-run leaf, and what it bypasses.
- [ward-kdl-tiers.md](ward-kdl-tiers.md) - the read/write/admin permission-tier layout.
- [ward-kdl-authoring.md](ward-kdl-authoring.md) - authoring guardfiles: getting the compiler, swapping the bundle.
- [guardfile-grammar.md](guardfile-grammar.md) - the dialect-1 KDL grammar, a minimal working guardfile, where auth config lives.
- [kdl-legibility.md](kdl-legibility.md) - the [ward#287](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/287) proposal to rename the quirky KDL tokens (`argv`, `$var`) to human-readable spellings.
- [config-source.md](config-source.md) - the `WARD_CONFIG_REF` fs.FS-at-launch seam: baked neutral default vs a live-resolved bundle.
- [config-ref-resolver.md](config-ref-resolver.md) - the `WARD_CONFIG_REF` git-ref grammar and its TTL-cached `syncGitRef` resolver.
- [fleet-local.md](fleet-local.md) - `~/.ward/fleet.local.kdl`, the operator-local config reader.
- [ward-kdl/](ward-kdl/) - 23 generated per-area guardfile references (git, aws, docker, the agents/ops/pkg surfaces, ...), indexed area-by-area from [ward-kdl-surface.md](ward-kdl-surface.md).

## Operator ops surfaces

- [ops-forgejo.md](ops-forgejo.md) - the forgejo spec/exec-verb proving ground.
- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - `ward ops forgejo`, the in-binary mount.
- [ops-forgejo-admin.md](ops-forgejo-admin.md) - the `{admin,doctor}` remote-exec slice.
- [ops-forgejo-view.md](ops-forgejo-view.md) - the lean `issue view` override.
- [ops-forgejo-quiet.md](ops-forgejo-quiet.md) - the `issue create --quiet` machine-output mode.
- [ops-forgejo-graft-removal.md](ops-forgejo-graft-removal.md) - the [ward#407](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/407) consult: per-graft design for removing the four Go grafts over the specverb tree.

## Examples

- [example-repo.md](example-repo.md) - the `examples/toy/` minimal ward-managed repo (Makefile + `.ward/ward.yaml` with a `security:` block + a ward-kdl guardfile), the demo and spec-bundle anchor.
- [demo.md](demo.md) - the runnable launch demo ([`../examples/demo.sh`](../examples/demo.sh)) driven against `examples/toy/`: one happy path, two danger classes.

## Release

- [release.md](release.md) - the Forgejo-canonical release pipeline.
- [release-binaries.md](release-binaries.md) - the dual-forge binary matrix + `SHA256SUMS` published to both release pages ([ward#454](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/454)).
- [forge-linking.md](forge-linking.md) - which forge a doc link should point at (relative / GitHub / Forgejo).

## See also

- [../README.md](../README.md) - human-facing intro.
- [../AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [FEATURES.md](FEATURES.md) - inventory of what ships today.
- [../.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
