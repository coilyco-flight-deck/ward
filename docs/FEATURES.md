# ward features

Baseline inventory of what `ward` ships, updated when a headline feature is added or reshaped.

## Scope

The contributor-facing cli-guard gate: a repo's dev verbs and audited host wrappers behind cli-guard's policy + audit pipeline.

## Commands

- **`ward exec <verb>`** - run a repo dev verb from `.ward/ward.yaml` through cli-guard's verb pipeline: argv-validated, one JSONL audit row per run (`repo.<verb>`), and a clean+synced tree gate (cli-guard `gittree`). `--audit-override-dirty` bypasses the gate (row tagged `audit_override=true`). See [docs/exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper at parity with `coily pkg brew`. Formula/tap mutations default to primary-org taps (`--allow-untapped` otherwise); read-only verbs and `brew bundle` pass through. Audit rows `pkg.brew.*`.
- **`ward upgrade`** - audited ward self-update: `brew update` + `brew upgrade coilyco-flight-deck/tap/ward` (`--dry` shows the diff). Audit row `upgrade`.
- **`ward audit {path,tail}`** - read surface over the per-repo audit log. `path` prints the resolved `~/.ward/audit/<slug>.jsonl`; `tail` streams rows as JSONL with `--since`, `--follow`, and `--scope`. See [docs/audit.md](audit.md).
- **`ward git <verb>`** - audited git verbs (cli-guard `passthrough`): status/log/diff/show/add/fetch/pull/push/branch/checkout/stash/restore as thin passthroughs (`git.<verb>`, leading `-C <dir>` hoisted). `ward git commit -m "msg" -- <path>...` is a dedicated concurrency-safe verb (private `GIT_INDEX_FILE`, explicit pathspecs, editor refused). See [docs/git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the resolved config + host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - register the PreToolUse hook in `.claude/settings.json` (idempotent).
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward dispatch <surface> <ref>`** - fire `claude` against a real open issue. Surfaces: `headless` (detached `claude -p` in a per-issue worktree+branch), `interactive`, `consult` (raised interruption budget), `cascade` (headless + bounded recursive sub-dispatch); maintenance `reap`/`status`/`registry`. Refs outside the primary-org set are refused; Forgejo refs resolve via a read-only SSM-token client. See [docs/dispatch.md](dispatch.md).
- **`ward container {up,exec,reap,down,ls}`** - ephemeral, least-access dev containers, one per `up`. `up [owner/name]` clones the target **fresh inside** (never on the host); the only host bind is the cwd, read-only. `--mode claude|codex|qwen` rides a context ladder; the push token is resolved host-side. It is its own permission manager (`bypassPermissions` + force-push deny). `reap` lands clean work on `main` or salvages it to a branch on exit. See [docs/container.md](container.md), [docs/container-reap.md](container-reap.md), [docs/container-substrate.md](container-substrate.md).

## Spec-driven ops (`ward-kdl`)

- **`ward-kdl ops <api> <verb>`** - guarded API verbs from KDL guardfiles (`specverb`): **forgejo** (Swagger 2.0, ward#109), **trello** (OpenAPI 3.0), **tailscale** (OpenAPI 3.1). Each `can` resolves its op by convention; denies teach, `restrict` scopes. Ships in ward's Homebrew formula, release-tag stamped (`ward-kdl --version`). See [docs/ops-forgejo.md](ops-forgejo.md).

## Scripts

- **`scripts/watch-ci.sh`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table and tail each failing job's log. Its poll loop now rides the `ci-watch` complex action. See [docs/ci-watch.md](ci-watch.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
