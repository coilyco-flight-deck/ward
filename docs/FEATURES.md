# ward features

Inventory of what `ward` ships, updated when a feature lands or reshapes.

## Scope

The contributor-facing cli-guard gate: repo dev verbs + audited host wrappers behind cli-guard's pipeline.

## Commands

- **`ward exec <verb>`** - run a repo dev verb from `.ward/ward.yaml` through cli-guard's pipeline: argv-validated, one JSONL audit row (`repo.<verb>`), clean+synced tree gate (`gittree`). `--audit-override-dirty` bypasses it. See [docs/exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper: formula/tap mutations default to primary-org taps (`--allow-untapped` otherwise), reads and `brew bundle` pass through. Rows `pkg.brew.*`.
- **`ward upgrade`** - audited ward self-update: `brew update` + `brew upgrade coilyco-flight-deck/tap/ward` (`--dry` shows the diff). Audit row `upgrade`.
- **`ward audit {path,tail}`** - read surface over the audit log. `path` prints the resolved `~/.ward/audit/<slug>.jsonl`; `tail` streams rows (`--since`/`--follow`/`--scope`). See [docs/audit.md](audit.md).
- **`ward git <verb>`** - audited git passthroughs (cli-guard `passthrough`): read + safe-write verbs (`git.<verb>`, `-C` hoisted). `ward git commit -m msg -- <path>...` is a concurrency-safe verb (private `GIT_INDEX_FILE`, explicit pathspecs, editor refused). See [docs/git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the resolved config + host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json` (idempotent).
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward dispatch <surface> <ref>`** - fire `claude` against a real open issue. Surfaces: `headless` (detached `claude -p`, per-issue worktree), `interactive`, `consult`, `cascade`; plus `reap`/`status`/`registry`. Off-org refs refused; Forgejo via read-only SSM-token client. See [docs/dispatch.md](dispatch.md).
- **`ward container {up,exec,reap,down,ls}`** - ephemeral, least-access dev containers, one per `up`: target cloned fresh inside (cwd bind, read-only), `--mode claude|codex|qwen|goose`, self-managed perms. `reap` lands clean work on `main` or salvages it. See [docs/container.md](container.md).
- **`ward agent <name> {work,headless,task}`** - shortcut over `container up`: `work`/`headless` take an existing issue (branch `issue-<N>`, carry-to-merge seed), `task` *files* one from `--instructions`. Off-org refused, `--print` dry-runs. Each run *reserves* the issue (local file sentinel + remote Forgejo marker comment, 2h TTL) so concurrent runs never double-work it; `--force` reclaims a stale hold. Interactive `headless` runs a fire-and-forget pre-flight feasibility check before detaching - GO launches, NO-GO comments on the issue and launches nothing, no prompt to answer (`--no-preflight` skips) (ward#147). A best-effort host-side reminder fires at dispatch when the host ward binary is behind the latest release, since a detached run buries the in-container `ward version` cue (ward#143). See [agent](agent.md).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - guarded API verbs from KDL guardfiles (`specverb`): **forgejo** (Swagger 2.0), **trello** (OpenAPI 3.0), **tailscale** (OpenAPI 3.1). Denies teach, `restrict` scopes. See [docs/ops-forgejo.md](ops-forgejo.md).
- **`ward-kdl ops {aws,kubectl} <verb>`** - guarded local-CLI passthroughs (`execverb`): **aws** (SSM/S3/EC2 reads, per-op resource guards) and **kubectl** (host-native; reads + apply/scale/rollout, destructive verbs unexposed). See [aws](ward-kdl.aws.guardfile.md), [kubectl](ward-kdl.kubectl.guardfile.md).
- **`ward-kdl agents <target> <verb>`** - mixed-transport. **`agents ui`**: the Open WebUI API (`specverb`, tailnet-only). **`agents {claude,codex,opencode,aider,goose}`**: local-CLI launchers (`execverb`), `argv`-override verbs. **`agents ollama`**: the tower's Ollama CLI (SSM OLLAMA_HOST).

## Scripts

- **`scripts/watch-ci.sh`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table and tail each failing job's log (rides the `ci-watch` action). See [docs/ci-watch.md](ci-watch.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
