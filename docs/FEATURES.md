# ward features

Inventory of what `ward` ships.

## Scope

Contributor-facing cli-guard gate: repo dev verbs + audited host wrappers.

## Commands

- **`ward exec <verb>`** - run a repo dev verb (`.ward/ward.yaml`) through cli-guard: argv-validated, one JSONL audit row, clean+synced tree gate. See [docs/exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper: formula/tap mutations default to primary-org taps (`--allow-untapped` else), reads pass through.
- **`ward upgrade`** - audited self-update via `brew upgrade coilyco-flight-deck/tap/ward` (`--dry`).
- **`ward audit {path,tail}`** - read surface over the audit log: `path` prints the log path, `tail` streams rows (`--since`/`--follow`). See [docs/audit.md](audit.md).
- **`ward git <verb>`** - audited git passthroughs: read + safe-write verbs (`-C` hoisted), plus a concurrency-safe `ward git commit -m msg -- <path>...` (private index). See [docs/git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the config + host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json`.
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward container {up,exec,reap,down,ls}`** - ephemeral, least-access dev containers, one per `up`: target cloned fresh inside (cwd bind read-only), `--mode claude|codex|qwen|goose`. `--with-repo owner/name` adds extra writable repos (ward#230). `reap` lands or salvages the work. See [docs/container.md](container.md), [docs/container-multi-repo.md](container-multi-repo.md).
- **`ward drive <harness> "<prompt>"`** (public face: **`warded <harness> "..."`**) - the canonical machinery behind the warded agent: a fresh least-access container fresh-clones the context repo and runs the named harness one-shot against the prompt, streaming inline. Every command is cli-guard-gated and audited - the boundary is the product. Off-org refused, `--print` dry-runs. The harness arg is the flag boundary: ward flags before it, prompt after raw (ward#248). `warded` is a thin `ward` symlink (ward#247). See [docs/drive.md](drive.md).
- **`ward agent {work,headless,task,reply,ask} [--driver <name>]`** - over `container up`: `work`/`headless` take an issue, `task` *files* one (ward#164), `reply` researches one-shot and comments back (ward#179), `ask` answers a freeform question inline. `--driver` picks the harness (`claude|codex|qwen|goose`, default claude; ward#185). Off-org refused, `--print` dry-runs; issue runs *reserve* it (2h TTL); `headless` pre-flights feasibility (ward#147); `--details` steers; `work --new-tab` opens its own Warp tab. See [agent](agent.md), [agent-reply](agent-reply.md), [agent-ask](agent-ask.md).
- **`ward ci watch [owner/repo]`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table linking each failing job. Native hand-written verb (audited, read-only). Exit `0`/`1`/`2`/`3` = passed/failed/timed-out/no-run (ward#88). See [docs/ci-watch.md](ci-watch.md).

## Spec-driven ops (`ward-kdl`)

`ward-kdl` carries the spec-driven (`specverb`) and passthrough (`execverb`) verb surfaces - `ops` (forgejo/trello/tailscale/glitchtip/signoz/aws/kubectl), `docker`, `agents`, and `pkg`. See [docs/ward-kdl-surface.md](ward-kdl-surface.md) for the per-surface breakdown. The in-binary `ward ops forgejo` also grafts a remote-exec admin/doctor slice (ward#81); see [ops-forgejo-admin](ops-forgejo-admin.md). Forgejo also ships as three permission-tiered binaries - `ward-kdl-{read,write,admin}` (ward#240).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
