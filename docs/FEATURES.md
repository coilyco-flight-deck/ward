# ward features

Inventory of what `ward` ships.

## Scope

Contributor-facing cli-guard gate: repo dev verbs + audited host wrappers.

## Commands

- **`ward exec <verb>`** - run a repo dev verb (`.ward/ward.yaml`) through cli-guard: argv-validated, one JSONL audit row, clean+synced tree gate. See [docs/exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper: formula/tap mutations default to primary-org taps (`--allow-untapped` else), reads pass through.
- **`ward upgrade`** - audited self-update via `brew upgrade coilyco-flight-deck/tap/ward` (`--dry`).
- **`ward audit {path,tail}`** - read surface over the audit log: `path` prints the log path, `tail` streams rows (`--since`/`--follow`). See [docs/audit.md](audit.md).
- **`ward git <verb>`** - audited git passthroughs (`-C` hoisted), concurrency-safe `ward git commit`, and destination-gated `ward git clone` (ward#285). See [git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the config + host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json`.
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward agent {work,headless,task,reply,ask,sandbox,explore} [--driver <name>]`** (public face: **`warded <surface> <ref>`**) - the dispatcher: `work`/`headless` take an issue, `task` files one (ward#164), `reply` comments back (ward#179), `ask` answers inline, `sandbox` is unguided (ward#292), `explore` its **read-only** twin (no push/salvage; ward#293/#299/#315). `warded` is a thin `ward` symlink for `ward agent` (ward#282); a **bare ref runs `headless`** and a bare `#N` infers `owner/repo`. `--driver` picks a harness (`claude|codex|qwen|goose`; ward#185). `--repo owner/name` adds writable repos (ward#230); `--host-net` joins the host tailnet route (native-Linux, ward#330/#332). Off-org refused, `--print` dry-runs; runs reserve (2h TTL); `headless` pre-flights (ward#147) + front-loads docs (ward#236); `--details` steers; `--new-tab` opens a tab. See [agent](agent.md), [agent-frontload](agent-frontload.md), [agent-sandbox](agent-sandbox.md), [agent-explore](agent-explore.md).
- **`ward container {reap,bootstrap}`** *(hidden, entrypoint-internal; ward#263)* - in-container plumbing: `reap` lands/salvages on teardown and verifies each `--repo` grant landed (reopens the issue on a silently-rejected secondary push, ward#291); `bootstrap` is the Go PID-1 entrypoint port (ward#181). See [reap](container-reap.md).
- **`ward ci watch [owner/repo]`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table linking each failing job. Native hand-written verb (audited, read-only). Exit `0`/`1`/`2`/`3` = passed/failed/timed-out/no-run (ward#88). See [docs/ci-watch.md](ci-watch.md).

## Spec-driven ops (`ward-kdl`)

`ward-kdl` is the build-time generator that compiles a guardfile into an audited CLI ([docs/ward-kdl.md](ward-kdl.md)). It carries the spec-driven and passthrough verb surfaces - `ops` (forgejo/trello/tailscale/glitchtip/signoz/aws/kubectl), `docker`, `agents`, and `pkg` ([ward-kdl-surface.md](ward-kdl-surface.md) breaks them down per-surface). The in-binary `ward ops forgejo` also grafts a remote-exec admin slice (ward#81, [ops-forgejo-admin](ops-forgejo-admin.md)) and ships as `ward-kdl-{read,write,admin}` permission tiers (ward#240).

The **exec-dialect** guardfiles auto-mount into `ward` at their `wrap` path with no per-guardfile graft. `git` / `pkg brew` keep hand-written surfaces (ward#284). See [in-ward](ward-kdl-in-ward.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
