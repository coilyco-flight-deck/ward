# ward features

Inventory of what `ward` ships.

## Scope

Contributor-facing cli-guard gate: repo dev verbs + audited host wrappers.

## Commands

- **`ward exec <verb>`** - run a repo dev verb (`.ward/ward.yaml`) through cli-guard: argv-validated, one JSONL audit row, clean+synced tree gate. See [exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper: formula/tap mutations default to primary-org taps (`--allow-untapped` else), reads pass through.
- **`ward upgrade`** - audited self-update via `brew upgrade coilyco-flight-deck/tap/ward` (`--dry`).
- **`ward audit {path,tail}`** - read surface over the audit log: `path` prints the log path, `tail` streams rows (`--since`/`--follow`). See [audit.md](audit.md).
- **`ward git <verb>`** - audited passthroughs, concurrency-safe `commit`, destination-gated `clone` (ward#285), ephemeral-clone search `grep`/`grep-remote` (ward#369). See [git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the config + host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json`.
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward agent {engineer,director,advisor} [--driver <name>]`** (public face: **`warded <role> <ref>`**) - a startup-role roster: `engineer` implements **detached**, `director` a heartbeat that **surfaces a read-only session** on drain + `--org`/config scope + **startup triage**, `advisor` answers without code (freeform interactive, `--oneshot` streams). `warded` is a `ward` symlink; a **bare ref runs `engineer`**, a bare `#N` infers `owner/repo`. `--driver` picks a harness; `--repo`/`--org` scope; off-org refused. `warded roster` lists roles. See [agent](agent.md).
- **`ward container {reap,bootstrap}`** *(hidden, entrypoint-internal; ward#263)* - in-container plumbing: `reap` lands/salvages on teardown and verifies each `--repo` grant landed (ward#291); `bootstrap` is the Go PID-1 entrypoint port (ward#181). See [reap](container-reap.md).
- **Agent-run observability** *(ward#363)* - the keep-10 sweep **drains** each exited run's console + transcript + secret-free `meta.json` to `~/.ward/agent-logs/<container>/` before `docker rm`. An opt-in extractor (`WARD_AGENT_TELEMETRY=1`, **default-OFF**) ships one **redacted envelope per tool call** as OTLP logs to SigNoz. See [agent-observability.md](agent-observability.md).
- **`ward ci watch [owner/repo]`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table linking each failing job. Audited, read-only. Exit `0`/`1`/`2`/`3` = passed/failed/timed-out/no-run (ward#88). See [ci-watch.md](ci-watch.md).
- **`ward agents list [--json]`** - dump the fleet roster from `fleetconfig.Fleet`; `--json` is the stable read surface aos reads (ward#417). See [agents-list](agents-list.md).

## Spec-driven ops (`ward-kdl`)

`ward-kdl` is the build-time authoring layer ([docs/ward-kdl.md](ward-kdl.md)). It carries permission surfaces and fleet configs: `ops` (forgejo/forgejo-key/trello/tailscale/glitchtip/signoz/aws/kubectl), `docker`, `agents`, and `pkg` ([ward-kdl-surface.md](ward-kdl-surface.md) per-surface), plus `ward-kdl.fleet.kdl`. The in-binary `ward ops forgejo` also grafts an admin slice ([ops-forgejo-admin](ops-forgejo-admin.md)) and ships as `ward-kdl-{read,write,admin}` tiers (ward#240).

The **exec-dialect** guardfiles auto-mount into `ward` at their `wrap` path with no per-guardfile graft. `git` / `pkg brew` keep hand-written surfaces (ward#284). See [in-ward](ward-kdl-in-ward.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
