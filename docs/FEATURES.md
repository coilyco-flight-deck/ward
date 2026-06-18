# ward features

Inventory of what `ward` ships.

## Scope

Contributor-facing cli-guard gate: repo dev verbs + audited host wrappers behind cli-guard's pipeline.

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
- **`ward dispatch <surface> <ref>`** - fire `claude` against a real open issue. Surfaces: `headless`, `interactive`, `consult`, `cascade`, plus `reap`/`status`/`registry`. Off-org refused. See [docs/dispatch.md](dispatch.md).
- **`ward container {up,exec,reap,down,ls}`** - ephemeral, least-access dev containers, one per `up`: target cloned fresh inside (cwd bind read-only), `--mode claude|codex|qwen|goose`. `reap` lands or salvages the work. See [docs/container.md](container.md).
- **`ward agent <name> {work,headless,task}`** - shortcut over `container up`: `work`/`headless` take an existing issue, `task` *files* one. Off-org refused, `--print` dry-runs; `headless`/`task` add an [agent commit suite](agent-precommit.md). Each run *reserves* the issue (2h TTL, `--force` reclaims) against double-work; `headless` pre-flights a fire-and-forget feasibility check (GO launches, NO-GO comments; ward#147). See [agent](agent.md).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - guarded API verbs from KDL guardfiles (`specverb`): **forgejo** (Swagger 2.0), **trello**/**tailscale** (OpenAPI 3.0/3.1). Denies teach, `restrict` scopes. See [docs/ops-forgejo.md](ops-forgejo.md).
- **`ward-kdl ops {aws,kubectl} <verb>`** - guarded local-CLI passthroughs (`execverb`): **aws** (SSM/S3/EC2 reads) and **kubectl** (reads + apply/scale/rollout; destructive verbs unexposed). See [aws](ward-kdl.aws.guardfile.md), [kubectl](ward-kdl.kubectl.guardfile.md).
- **`ward-kdl agents <target> <verb>`** - mixed-transport. **`agents ui`**: Open WebUI API (`specverb`, tailnet-only). **`agents {claude,codex,opencode,aider,goose}`**: local-CLI launchers (`execverb`, `argv`-override). **`agents ollama`**: the tower's Ollama.
- **`ward-kdl pkg <api> <resource> <verb>`** - package-directory lookups (`specverb`): **skillsmp** (skill discovery) and **glama** (Glama MCP directory), migrated from `coily pkg` per ward#105. See [skillsmp](ward-kdl.skillsmp.guardfile.md), [glama](ward-kdl.glama.guardfile.md).

## Scripts

- **`scripts/watch-ci.sh`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table and tail each failing job's log (`ci-watch` action). See [docs/ci-watch.md](ci-watch.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
