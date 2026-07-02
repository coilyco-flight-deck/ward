# ward features

Inventory of what `ward` ships.

## Scope

Contributor-facing cli-guard gate: repo dev verbs + audited host wrappers.

## Commands

- **`ward exec <verb>`** - run a repo dev verb (`.ward/ward.yaml`) through cli-guard: argv-validated, one JSONL audit row, clean+synced gate. See [exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper: formula/tap mutations default to primary-org taps (`--allow-untapped` else); reads pass through.
- **`ward upgrade`** - audited self-update via `brew upgrade coilyco-flight-deck/tap/ward` (`--dry`).
- **`ward audit {path,tail}`** - read the audit log: `path` prints its path, `tail` streams rows (`--since`/`--follow`). See [audit.md](audit.md).
- **`ward git <verb>`** - audited passthroughs, concurrency-safe `commit`, destination-gated `clone` ([ward#285](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/285)), ephemeral-clone `grep`/`grep-remote` ([ward#369](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/369)). See [git-verbs.md](git-verbs.md).
- **`ward setup`** (`warded setup`) - scaffold `.ward/ward.yaml` from the Makefile, run doctor. See [setup.md](setup.md).
- **`ward doctor`** - diagnostic checks against the config + host: the allowlist drift guard, the host security probes, and ([ward#450](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/450)) a fail-by-default gate when a repo declares no `security:` block. See [doctor.md](doctor.md).
- **`ward exec lint-refs`** - lint issue refs in public-facing docs so every one resolves for a GitHub reader ([ward#446](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/446)): a terse `ward#123` never autolinks in a rendered Markdown file on the mirror, so `scripts/lint_issue_refs.py` (also a pre-commit gate) requires full-URL links and `--fix` rewrites them.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json`.
- **`ward agent {engineer,director,advisor} [--driver <name>]`** (public face: **`warded <role> <ref>`**) - startup-role roster: `engineer` implements detached, `director` a heartbeat surfacing a read-only session + triage, `advisor` answers without code (a ref comments or fans cross-repo work into per-repo issues, trust-gated; [ward#424](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/424)). Every role dispatches only for a compiled-in [trusted owner](agent-trust-gate.md). See [agent](agent.md).
- **`ward container {reap,bootstrap}`** *(hidden, entrypoint-internal; [ward#263](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/263))* - in-container plumbing: `reap` lands/salvages on teardown, records dispatch-time provenance, requires proof that the run landed new history after the reservation before it can read as done, enforces the carried issue's same-repo `closes #N` before `main` can land, short-circuits a landed-and-closed run as nothing-to-reap before any salvage gate so it cannot false-salvage, posts a genuine salvage notice back on the carried issue (reopening it) instead of a standalone mega-issue ([ward#518](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/518)), dumps a structured `--- reap diagnostics ---` block (reaper ward version + resolution, HEAD-vs-`origin/main` ancestry, decision gate, provenance/landed state) to stderr and the salvage notification on every salvage/failure ([ward#531](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/531)); the reaper's own correctness fixes are protected at dispatch by a version guard that refuses to install a ward **older** than the dispatching host unless `--allow-ward-downgrade` is passed, so a stale pin cannot silently ship an already-fixed false-salvage bug into the container ([ward#529](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/529)), and verifies each `--repo` grant landed ([ward#291](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/291)); `bootstrap` is the PID-1 entrypoint port ([ward#181](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/181)). See [reap](container-reap.md) and [provenance](container-reap-provenance.md).
- **Agent-run observability** *([ward#363](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/363))* - the keep-10 sweep drains each exited run's logs + secret-free `meta.json` to `~/.ward/agent-logs/` before `docker rm`; ward now also emits stable lifecycle lines during dispatch, bootstrap, and reap for headless debugging; opt-in `WARD_AGENT_TELEMETRY=1` (default-OFF) ships redacted per-tool-call OTLP logs to SigNoz; the [director read-only surface](agent-surface.md) binds that drain dir read-only at `/opt/ward-agent-logs` so a director reads run logs without a docker socket ([ward#525](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/525)). See [agent-observability.md](agent-observability.md).
- **`ward agents list [--json]`** - dump the fleet roster from `fleetconfig.Fleet`; `--json` is the stable read surface [agentic-os](https://github.com/coilysiren/agentic-os) (`aos`) reads ([ward#417](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/417)). See [agents-list](agents-list.md).

## Agent harness seam

- **Per-agent folders own their behaviour** *([ward#425](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/425))* - each harness is an `agentsapi.Agent` in `internal/agents/<name>/` owning its capability bodies; core dispatches through the registry by feature-test (no per-agent `switch`); a ratchet blocks its return. See [agentsapi.md](agentsapi.md).

## Spec-driven ops (`ward-kdl`)

`ward-kdl` is the build-time authoring layer ([docs/ward-kdl.md](ward-kdl.md)): permission surfaces + fleet configs for `ops` (forgejo/tailscale/signoz/aws/kubectl/...), `docker`, `agents`, `pkg` ([surface](ward-kdl-surface.md)), plus `ward-kdl.fleet.kdl`. It builds into `ward-kdl-{read,write,admin}` tiers ([ward#240](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/240)) - not public artifacts ([ward#455](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/455)), spec authors build from a clone ([authoring](ward-kdl-authoring.md)).

The **exec-dialect** guardfiles auto-mount at their `wrap` path; `git` / `pkg brew` keep hand-written surfaces ([ward#284](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/284)). See [in-ward](ward-kdl-in-ward.md).

## Release pipeline

- **Release notes** *([ward#486](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/486))* - "does it affect you" verdict. [release-notes.md](release-notes.md).
- **Dual-forge binary matrix** *([ward#454](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/454))* - `ward-{darwin,linux}-{amd64,arm64}` + `SHA256SUMS`, built once per tag and published byte-identical to both the Forgejo and GitHub release pages, so their checksums match. [release.md](release.md), [github-mirror.md](github-mirror.md).

## See also

- [README.md](../README.md) - human intro.
- [AGENTS.md](../AGENTS.md) - operating rules.
- [docs index](README.md) - docs by subsystem.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
