# ward features

Inventory of what `ward` ships.

## Scope

Contributor-facing cli-guard gate: repo dev verbs + audited host wrappers.

## Commands

- **`ward exec <verb>`** - run a repo dev verb (`.ward/ward.yaml`) through cli-guard: argv-validated, one JSONL audit row, clean+synced gate. See [exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper: formula/tap mutations default to primary-org taps (`--allow-untapped` else); reads pass through.
- **`ward upgrade`** - audited self-update via `brew upgrade coilyco-flight-deck/tap/ward` (`--dry`).
- **`ward audit {path,tail}`** - read the audit log: `path` prints its path, `tail` streams rows (`--since`/`--follow`). See [audit.md](audit.md).
- **`ward git <verb>`** - audited passthroughs, concurrency-safe `commit`, destination-gated `clone` (ward#285), ephemeral-clone `grep`/`grep-remote` (ward#369). See [git-verbs.md](git-verbs.md).
- **`ward setup`** (`warded setup`) - scaffold `.ward/ward.yaml` from the Makefile, run doctor. See [setup.md](setup.md).
- **`ward doctor`** - diagnostic checks against the config + host, including the allowlist drift guard. See [doctor.md](doctor.md).
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json`.
- **`ward agent {engineer,director,advisor} [--driver <name>]`** (public face: **`warded <role> <ref>`**) - startup-role roster: `engineer` implements detached, `director` a heartbeat surfacing a read-only session + triage, `advisor` answers without code (a ref comments or fans cross-repo work into per-repo issues, trust-gated; ward#424). Every role dispatches only for a compiled-in [trusted owner](agent-trust-gate.md). See [agent](agent.md).
- **`ward container {reap,bootstrap}`** *(hidden, entrypoint-internal; ward#263)* - in-container plumbing: `reap` lands/salvages on teardown + verifies each `--repo` grant landed (ward#291); `bootstrap` is the PID-1 entrypoint port (ward#181). See [reap](container-reap.md).
- **Agent-run observability** *(ward#363)* - the keep-10 sweep drains each exited run's logs + secret-free `meta.json` to `~/.ward/agent-logs/` before `docker rm`; opt-in `WARD_AGENT_TELEMETRY=1` (default-OFF) ships redacted per-tool-call OTLP logs to SigNoz. See [agent-observability.md](agent-observability.md).
- **`ward agents list [--json]`** - dump the fleet roster from `fleetconfig.Fleet`; `--json` is the stable read surface aos reads (ward#417). See [agents-list](agents-list.md).

## Agent harness seam

- **Per-agent folders own their behaviour** *(ward#425)* - each harness is an `agentsapi.Agent` in `internal/agents/<name>/` owning its capability bodies; core dispatches through the registry by feature-test (no per-agent `switch`); a ratchet blocks its return. See [agentsapi.md](agentsapi.md).

## Spec-driven ops (`ward-kdl`)

`ward-kdl` is the build-time authoring layer ([docs/ward-kdl.md](ward-kdl.md)): permission surfaces + fleet configs for `ops` (forgejo/tailscale/signoz/aws/kubectl/...), `docker`, `agents`, `pkg` ([surface](ward-kdl-surface.md)), plus `ward-kdl.fleet.kdl`. It builds into `ward-kdl-{read,write,admin}` tiers (ward#240) - not public artifacts (ward#455), spec authors build from a clone ([authoring](ward-kdl-authoring.md)).

The **exec-dialect** guardfiles auto-mount at their `wrap` path; `git` / `pkg brew` keep hand-written surfaces (ward#284). See [in-ward](ward-kdl-in-ward.md).

## Release pipeline

- **Release notes** *(ward#486)* - "does it affect you" verdict. [release-notes.md](release-notes.md).

## See also

- [README.md](../README.md) - human intro.
- [AGENTS.md](../AGENTS.md) - operating rules.
- [docs index](README.md) - docs by subsystem.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
