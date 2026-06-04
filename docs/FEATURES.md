# ward features

Baseline inventory of what `ward` ships. Update when a headline feature is added, removed, or materially reshaped.

## Scope

The contributor-facing cli-guard gate: wrap a repo's dev verbs and a small set of audited host wrappers behind cli-guard's policy + audit pipeline.

## Commands

- **`ward exec <verb>`** - run a repo dev verb from `.ward/ward.yaml`, argv-validated against cli-guard policy.
- **`ward pkg brew <verb>`** - audited brew wrapper at parity with `coily pkg brew`. Mirrors brew's argv; formula/tap mutations default to primary-org taps and need `--allow-untapped` otherwise; read-only verbs and `brew bundle` pass through. Audit rows `pkg.brew.*` to `~/.coily/audit/<repo>.jsonl`. The ward-native package path for board repos.
- **`ward doctor`** - diagnostic checks against the resolved config and host.
- **`ward hook pre-tool-use`** - Claude Code PreToolUse hook: binary-path check + bare-command deny with routing hints.
- **`ward install-hooks`** - idempotently register the PreToolUse hook in `.claude/settings.json`.
- **`ward lint`** - lint `.ward/ward.yaml` against the repo Makefile.

## See also

- [README.md](../README.md) - human-facing intro.
- [AGENTS.md](../AGENTS.md) - agent-facing operating rules.
- [.ward/ward.yaml](../.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
