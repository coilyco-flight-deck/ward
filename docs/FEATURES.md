# ward features

Inventory of what `ward` ships, updated when a feature is added or reshaped.

## Scope

The contributor-facing cli-guard gate: a repo's dev verbs and audited host wrappers behind cli-guard's policy + audit pipeline.

## Commands

- **`ward exec <verb>`** - run a repo dev verb from `.ward/ward.yaml` through cli-guard's pipeline: argv-validated, one JSONL audit row (`repo.<verb>`), clean+synced tree gate (`gittree`). `--audit-override-dirty` bypasses it (row tagged `audit_override=true`). See [docs/exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper at parity with `coily pkg brew`: formula/tap mutations default to primary-org taps (`--allow-untapped` otherwise), read-only verbs and `brew bundle` pass through. Rows `pkg.brew.*`.
- **`ward upgrade`** - audited ward self-update: `brew update` + `brew upgrade coilyco-flight-deck/tap/ward` (`--dry` shows the diff). Audit row `upgrade`.
- **`ward audit {path,tail}`** - read surface over the per-repo audit log. `path` prints the resolved `~/.ward/audit/<slug>.jsonl`; `tail` streams rows as JSONL with `--since`, `--follow`, and `--scope`. See [docs/audit.md](audit.md).
- **`ward git <verb>`** - audited git passthroughs (cli-guard `passthrough`): status/log/diff/show/add/fetch/pull/push/branch/checkout/stash/restore (`git.<verb>`, leading `-C <dir>` hoisted). `ward git commit -m "msg" -- <path>...` is a dedicated concurrency-safe verb (private `GIT_INDEX_FILE`, explicit pathspecs, editor refused). See [docs/git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the resolved config + host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json` (idempotent).
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward dispatch <surface> <ref>`** - fire `claude` against a real open issue. Surfaces: `headless` (detached `claude -p` in a per-issue worktree+branch), `interactive`, `consult`, `cascade` (bounded recursive sub-dispatch); plus `reap`/`status`/`registry`. Non-primary-org refs are refused; Forgejo refs use a read-only SSM-token client. See [docs/dispatch.md](dispatch.md).
- **`ward container {up,exec,reap,down,ls}`** - ephemeral, least-access dev containers, one per `up`: the target is cloned fresh inside (the only host bind is the cwd, read-only), `--mode claude|codex|qwen` rides a context ladder, the push token resolves host-side, and it manages its own permissions (`bypassPermissions` + force-push deny). `reap` lands clean work on `main` or salvages it to a branch on exit. See [docs/container.md](container.md) (+ [reap](container-reap.md), [substrate](container-substrate.md)).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - guarded API verbs from KDL guardfiles (`specverb`): **forgejo** (Swagger 2.0), **trello** (OpenAPI 3.0), and **tailscale** (OpenAPI 3.1). Each `can` resolves its op by convention; denies teach, `restrict` scopes. See [docs/ops-forgejo.md](ops-forgejo.md).
- **`ward-kdl agents <target> <verb>`** - mixed-transport agent surface. **`agents ui`**: the Open WebUI API (`specverb`, tailnet-only). **`agents {claude,codex,opencode,aider,goose}`**: local-CLI launchers (`execverb`); `launch`/`headless`/`login`/`whoami` map to each tool's real invocation via per-grant `argv` overrides.

## Scripts

- **`scripts/watch-ci.sh`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table and tail each failing job's log (rides the `ci-watch` action). See [docs/ci-watch.md](ci-watch.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
