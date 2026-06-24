# ward features

Inventory of what `ward` ships.

## Scope

Contributor-facing cli-guard gate: repo dev verbs + audited host wrappers.

## Commands

- **`ward exec <verb>`** - run a repo dev verb (`.ward/ward.yaml`) through cli-guard: argv-validated, one JSONL audit row, clean+synced tree gate (`--audit-override-dirty` bypasses). See [docs/exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper: formula/tap mutations default to primary-org taps (`--allow-untapped` else), reads + `brew bundle` pass through.
- **`ward upgrade`** - audited self-update: `brew update` + `brew upgrade coilyco-flight-deck/tap/ward` (`--dry`).
- **`ward audit {path,tail}`** - read surface over the audit log: `path` prints `~/.ward/audit/<slug>.jsonl`, `tail` streams rows (`--since`/`--follow`). See [docs/audit.md](audit.md).
- **`ward git <verb>`** - audited git passthroughs: read + safe-write verbs (`-C` hoisted), plus a concurrency-safe `ward git commit -m msg -- <path>...` (private index, explicit pathspecs). See [docs/git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the config + host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json` (idempotent).
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward container {up,exec,reap,down,ls}`** - ephemeral, least-access dev containers, one per `up`: target cloned fresh inside (cwd bind read-only), `--mode claude|codex|qwen|goose`. `--with-repo owner/name` grants extra writable repos cloned full alongside the target (multi-repo runs, ward#230). `reap` lands or salvages the work. See [docs/container.md](container.md), [docs/container-multi-repo.md](container-multi-repo.md).
- **`ward agent {work,headless,task,reply,ask} [--driver <name>]`** - over `container up`: `work`/`headless` take an issue, `task` *files* one (ward#164), `reply` researches one-shot and comments back (no container; ward#179), `ask` answers a freeform question one-shot in a fresh container, streamed inline (no issue/comment). `--driver` picks the harness (`claude|codex|qwen|goose`, default claude; ward#185 moved it off a subcommand slot to free a future `--reviewer`). Off-org refused, `--print` dry-runs; `headless`/`task` add an [agent commit suite](agent-precommit.md). Issue runs *reserve* it (2h TTL, `--force` reclaims); `headless` pre-flights feasibility (ward#147, #159). `--details` adds a steer (ward#167). `work --new-tab` spawns its own Warp tab (ward#174). See [agent](agent.md), [agent-reply](agent-reply.md), [agent-ask](agent-ask.md).
- **`ward ci watch [owner/repo]`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table and link each failing job's run page. Native hand-written verb (the poll loop is composite control flow the deny-by-default specverb engine can't host); audited, read-only. Exit `0`/`1`/`2`/`3` = passed/failed/timed-out/no-run. Replaces the retired `scripts/watch-ci.sh` (ward#88). See [docs/ci-watch.md](ci-watch.md).

## Spec-driven ops (`ward-kdl`)

`ward-kdl` carries the spec-driven (`specverb`) and passthrough (`execverb`) verb surfaces - `ops` (forgejo/trello/tailscale/aws/kubectl), `docker`, `agents`, and `pkg`. See [docs/ward-kdl-surface.md](ward-kdl-surface.md) for the per-surface breakdown. The in-binary `ward ops forgejo` also grafts a remote-exec admin/doctor slice (ward#81); see [ops-forgejo-admin](ops-forgejo-admin.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
