# ward features

Baseline inventory of what `ward` ships. Update when a headline feature is added, removed, or materially reshaped.

## Scope

The contributor-facing cli-guard gate: wrap a repo's dev verbs and a small set of audited host wrappers behind cli-guard's policy + audit pipeline.

## Commands

- **`ward exec <verb>`** - run a repo dev verb from `.ward/ward.yaml` through cli-guard's verb pipeline: argv-validated against the shell-metacharacter policy, one JSONL audit row per run (`repo.<verb>`), and a clean+synced tree gate (cli-guard `gittree`) that refuses when the declaring `ward.yaml` is uncommitted or the branch is out of sync (unrelated dirt is captured, not refused). `ward --audit-override-dirty exec <verb>` bypasses the gate, tagging the row `audit_override=true`. See [docs/exec-verb.md](exec-verb.md).
- **`ward pkg brew <verb>`** - audited brew wrapper at parity with `coily pkg brew`. Mirrors brew's argv; formula/tap mutations default to primary-org taps and need `--allow-untapped` otherwise; read-only verbs and `brew bundle` pass through. Audit rows `pkg.brew.*` to `~/.ward/audit/<repo>.jsonl`. The ward-native package path for board repos.
- **`ward upgrade`** - audited ward self-update: `brew update` + `brew upgrade coilyco-flight-deck/ward/ward` (`--dry` shows the version diff via `brew outdated`). Bound to ward-the-formula; `ward pkg brew upgrade` is the path for any other formula. Audit row `upgrade`.
- **`ward audit {path,tail}`** - read surface over the per-repo audit log. `path` prints the resolved `~/.ward/audit/<slug>.jsonl`; `tail` streams rows as JSONL with `--since` (unix seconds or `5m`/`7d`), `--follow`, and `--scope` (filter by `repo_root`; `.`/`here` resolve to the git toplevel). See [docs/audit.md](audit.md).
- **`ward git <verb>`** - audited git verbs for contributors (cli-guard `passthrough`). `status`, `log`, `diff`, `show`, `add`, `fetch`, `pull`, `push`, `branch`, `checkout`, `stash`, `restore` are thin audited passthroughs (`git.<verb>`); a leading `-C <dir>` is hoisted ahead of the subcommand. `ward git commit -m "msg" -- <path>...` is a dedicated concurrency-safe verb (private `GIT_INDEX_FILE`, explicit pathspecs, editor refused) so co-located sessions can't cross commits. See [docs/git-verbs.md](git-verbs.md).
- **`ward doctor`** - diagnostic checks against the resolved config and host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - idempotently register the PreToolUse hook in `.claude/settings.json`.
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.
- **`ward dispatch <surface> <ref>`** - fire `claude` against a real open issue. Four surfaces: `headless` (detached `claude -p` in a per-issue worktree+branch, fire-and-forget), `interactive` (new tab in the canonical checkout, supervised), `consult` (interactive with a raised interruption budget), `cascade` (headless plus bounded recursive sub-dispatch). Maintenance verbs `reap`/`status`/`registry`. Refs outside the primary-org set are refused; Forgejo refs resolve via a read-only client (token from SSM), falling back to GitHub for shortform refs. The contributor-facing home for what was `coily dispatch`. See [docs/dispatch.md](dispatch.md).

## Scripts

- **`scripts/watch-ci.sh`** - watch a Forgejo Actions run until every job is terminal, then print a per-job status table and tail each failing job's log. Its poll loop now has a native home: the `ci-watch` complex action; the script narrows to the bridge for run-defaulting and log tails. End-state: a native `ward ci watch` verb. See [docs/ci-watch.md](ci-watch.md).

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
