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
- [doctor.md](doctor.md) - `ward doctor`, the single diagnostic verb, incl. the allowlist drift guard.
- [audit.md](audit.md) - the append-only JSONL audit row written per invocation.
- [config-discovery.md](config-discovery.md) - how ward resolves the allowlist config path.
- [golangci.md](golangci.md) - the strict-ish golangci-lint configuration.
- [homebrew-build.md](homebrew-build.md) - Homebrew build + cli-guard pinning notes.

## Contributor dev-verb gate

- [exec-verb.md](exec-verb.md) - `ward exec <verb>`: run a repo dev verb through the gate.
- [verb-fallback.md](verb-fallback.md) - unknown-verb rewrite to `ward exec` + the build/test/install triple.
- [git-verbs.md](git-verbs.md) - `ward git`: audited, concurrency-safe git surface.
- [git-clone.md](git-clone.md) - `ward git clone`, destination-gated.
- [hook.md](hook.md) - `ward hook`, the Claude Code hook entry points.
- [install-hooks.md](install-hooks.md) - `ward install-hooks`, PreToolUse hook registration.

## `ward agent` (headless harness runner)

- [agent.md](agent.md) - the entrypoint to the ephemeral container that carries a feature.
- [agent-subcommands.md](agent-subcommands.md) - how the roles differ (what they do, attachment, scope).
- [agent-roster.md](agent-roster.md) - the generated role roster.
- [agent-engineer.md](agent-engineer.md) - the implement-a-ticket role.
- [agent-director.md](agent-director.md) - the autonomous-backlog heartbeat role.
- [agent-advisor.md](agent-advisor.md) - the counsel role that answers without writing code.
- [agent-advisor-fanout.md](agent-advisor-fanout.md) - advisor ref mode: structured emit + cross-repo fan-out.
- [agent-flags.md](agent-flags.md) - launch flags for the `engineer` role.
- [agent-frontload.md](agent-frontload.md) - front-loading subsystem context before detach.
- [agent-preflight.md](agent-preflight.md) - the headless pre-flight before a fire-and-forget run.
- [agent-wrong-repo.md](agent-wrong-repo.md) - the WRONG-REPO blind-fire pre-flight guard.
- [agent-reservation.md](agent-reservation.md) - reservation + stale-host-binary checks.
- [agent-reserved-immutable.md](agent-reserved-immutable.md) - a reserved seed is immutable.
- [agent-gate.md](agent-gate.md) - the interactive pre-launch gate.
- [agent-surface.md](agent-surface.md) - the director's read-only scope-and-dispatch surface.
- [director-startup-triage.md](director-startup-triage.md) - director startup triage (autonomous drain).
- [director-on-demand-surface.md](director-on-demand-surface.md) - the director's on-demand surface.
- [broker.md](broker.md) - the root credential broker that hardens the director's surface.
- [agent-credentials.md](agent-credentials.md) - how each harness's host credential is seeded.
- [agent-attribution.md](agent-attribution.md) - agent attribution on Forgejo write bodies.
- [agent-observability.md](agent-observability.md) - agent-run log/telemetry drain + opt-in OTLP.
- [agent-host-net.md](agent-host-net.md) - `--tailnet`, the opt-in network escalation.
- [agent-ts-sidecar.md](agent-ts-sidecar.md) - the Docker Desktop tailnet-route sidecar.
- [agent-adapter-manifest.md](agent-adapter-manifest.md) - the per-agent divergence manifest.
- [agentsapi.md](agentsapi.md) - `internal/agentsapi`, the agent-agnostic seam contract.
- [agents-list.md](agents-list.md) - `ward agents list`, the fleet-roster read surface.

## Agent harnesses (drivers)

- [agent-local-harnesses.md](agent-local-harnesses.md) - index of the local harness pages.
- [agent-claude.md](agent-claude.md) - the `claude` full cloud harness.
- [agent-codex.md](agent-codex.md) - the `codex` open-sandbox cloud harness.
- [agent-opencode.md](agent-opencode.md) - the `opencode` local Ollama-backed harness.
- [agent-goose.md](agent-goose.md) - the `goose` local harness.

## Container subsystem

- [container.md](container.md) - the ephemeral, least-access dev container.
- [container-permissions.md](container-permissions.md) - what the container itself may do.
- [container-substrate.md](container-substrate.md) - the read-only `/substrate` reference repos.
- [container-multi-repo.md](container-multi-repo.md) - multi-repo runs (`--repo`).
- [container-precommit.md](container-precommit.md) - fresh-clone pre-commit parity.
- [container-reap.md](container-reap.md) - `ward container reap`, the teardown backstop.
- [container-cleanup.md](container-cleanup.md) - cleaning up stopped containers.
- [container-stop.md](container-stop.md) - halting a running container.

## ward-kdl & config (build-time authoring layer)

- [ward-kdl.md](ward-kdl.md) - what ward-kdl is: the build-time authoring layer.
- [ward-kdl-surface.md](ward-kdl-surface.md) - the full generated verb surface, area by area.
- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - exec guardfiles auto-mounted into `ward`.
- [ward-kdl-tiers.md](ward-kdl-tiers.md) - the read/write/admin permission-tier layout.
- [ward-kdl-authoring.md](ward-kdl-authoring.md) - authoring guardfiles.
- [ward-kdl.brew.scoped.md](ward-kdl.brew.scoped.md) - why `ward pkg brew` scoped verbs stay gated Go.
- [fleet-local.md](fleet-local.md) - `~/.ward/fleet.local.kdl`, the operator-local config reader.
- [ward-kdl/](ward-kdl/) - 24 generated per-area guardfile references (git, aws, docker, the agents/ops/pkg surfaces, ...), indexed area-by-area from [ward-kdl-surface.md](ward-kdl-surface.md).

## Operator ops surfaces

- [ops-forgejo.md](ops-forgejo.md) - the forgejo spec/exec-verb proving ground.
- [ops-forgejo-in-ward.md](ops-forgejo-in-ward.md) - `ward ops forgejo`, the in-binary mount.
- [ops-forgejo-admin.md](ops-forgejo-admin.md) - the `{admin,doctor}` remote-exec slice.
- [ops-forgejo-view.md](ops-forgejo-view.md) - the lean `issue view` override.
- [ops-forgejo-quiet.md](ops-forgejo-quiet.md) - the `issue create --quiet` machine-output mode.

## Release & CI

- [release.md](release.md) - the Forgejo-canonical release pipeline.
- [ci-watch.md](ci-watch.md) - `ward ci watch`, watch a Forgejo Actions run to terminal.

## See also

- [../README.md](../README.md) - human-facing intro.
- [../AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [FEATURES.md](FEATURES.md) - inventory of what ships today.
- [../.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
