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
- **`ward container {up,exec,reap,down,ls}`** - ephemeral, least-access dev containers, one per `up`: target cloned fresh inside (cwd bind read-only), `--mode claude|codex|qwen|goose`. `reap` lands or salvages the work. See [docs/container.md](container.md).
- **`ward agent <name> {work,headless,task,reply}`** - over `container up`: `work`/`headless` take an issue, `task` *files* one (DIRECT, or ROUTE; ward#164), `reply` researches one-shot and posts the result as a comment (quick|standard|deep, no container; ward#179). Off-org refused, `--print` dry-runs; `headless`/`task` add an [agent commit suite](agent-precommit.md). Each run *reserves* the issue (2h TTL, `--force` reclaims); `headless` pre-flights a feasibility check (GO/NO-GO/blind-fire; ward#147, ward#159). `--details` adds an authoritative steer (ward#167). `work --new-tab` spawns the run into its own Warp tab (ward#174). See [agent](agent.md), [agent-reply](agent-reply.md).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - `specverb` API verbs: **forgejo** (Swagger 2.0), **trello**/**tailscale** (OpenAPI 3.0/3.1). Denies teach, `restrict` scopes. See [docs/ops-forgejo.md](ops-forgejo.md).
- **`ward-kdl ops {aws,kubectl} <verb>`** - `execverb` local-CLI passthroughs: **aws** (SSM/S3/EC2 reads) and **kubectl** (reads + apply/scale/rollout; destructive verbs unexposed). See [aws](ward-kdl.aws.guardfile.md), [kubectl](ward-kdl.kubectl.guardfile.md).
- **`ward-kdl agents <target> <verb>`** - mixed-transport. **`agents {claude,codex,opencode,aider,goose}`**: local-CLI launchers (`execverb`, `argv`-override). **`agents ollama`**: the tower's Ollama.
- **`ward-kdl pkg <resource> <verb>`** - `specverb` package-directory lookups: **skillsmp** (skills) and **glama** (Glama MCP), from `coily pkg` (ward#105); plus **`ward-kdl pkg brew <verb>`** - brew reads/passthrough (`execverb`, jailed; scoped verbs stay Go, [ward#95](ward-kdl.brew.scoped.md)). See [skillsmp](ward-kdl.skillsmp.guardfile.md), [glama](ward-kdl.glama.guardfile.md).

## Scripts

- **`scripts/watch-ci.sh`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table and tail each failing job's log (`ci-watch` action). See [docs/ci-watch.md](ci-watch.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
