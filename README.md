# agent-guard

A generic [cli-guard][cli-guard] consumer for repos that take external contributions. Sits between AI agents (or any semi-trusted automation) and the host system, with no maintainer-specific allowlists.

`agent-guard` is to external contributors what [coily][coily] is to Kai's own machines: a thin, audited wrapper around cli-guard primitives. coily ships personal verbs (homelab SSH, vault paths, deploy hooks). `agent-guard` ships only verbs that make sense to any contributor walking up to a repo cold.

## Status

v0. Not yet wired into any downstream. First adopter target is the urfave/cli namespaced repos ([cli-mcp][cli-mcp], [cli-web-docs][cli-web-docs], [cli-web-ops][cli-web-ops]).

## What it does

Wraps a small fixed set of dev verbs (`build`, `test`, `vet`, `lint`, `tidy`) behind cli-guard's policy gate. Every invocation validates argv, writes one append-only JSONL audit row, binds to a git toplevel via `--commit-scope`, and refuses repo-shaped verbs on a dirty tree.

Downstream repos add `.agent-guard/agent-guard.yaml` listing which Makefile targets are exposed. The contract is verified by `agent-guard lint`.

## Install

```
brew tap coilysiren/agent-guard https://forgejo.coilysiren.me/coilysiren/agent-guard
brew install coilysiren/agent-guard/agent-guard
```

The explicit-URL `brew tap` form is required because this repo isn't `homebrew-*` prefixed.

## Usage

```
agent-guard exec build
agent-guard exec test
agent-guard lint
```

See [`docs/`](docs/) for the full verb list and [`examples/`](examples/) for runnable demos.

## Claude Code PreToolUse hook

`agent-guard hook pre-tool-use` is a stdin-driven [Claude Code hook](https://docs.claude.com/en/docs/claude-code/hooks). It does two things:

1. **Binary-path check.** Refuses to let `agent-guard` or `coily` run unless `command -v` resolves to a canonical homebrew install path. Blocks PATH-hijack attacks. On by default, no flag.
2. **Routing-hint surface.** Catches bare invocations of wrapped binaries (`make`, `gh`, `aws`, `kubectl`, ...) and surfaces a recovery hint naming the right wrapper. The active table is picked by `.agent-guard/agent-guard.yaml` vs `.coily/coily.yaml` in cwd.

No network, no state. Failure modes pass through silently. Hard denial stays the job of `permissions.deny`.

Register with `agent-guard install-hooks` (idempotent). Writes the PreToolUse entry to `<git-toplevel>/.claude/settings.json`. Flags: `--path <file>`, `--dry-run`, `--check`.

## Related

- [cli-guard][cli-guard] - underlying security-boundary framework
- [coily][coily] - Kai's personal cli-guard consumer
- Sibling cli-* repos: [cli-mcp][cli-mcp], [cli-web-docs][cli-web-docs], [cli-web-ops][cli-web-ops]

## Support

Bug or feature request: [create a new issue][new-issue]. Conduct: [Code of Conduct](CODE_OF_CONDUCT.md). Security: [SECURITY.md](SECURITY.md). License: [`LICENSE`](./LICENSE).

[cli-guard]: https://github.com/coilysiren/cli-guard
[coily]: https://github.com/coilysiren/coily
[cli-mcp]: https://github.com/coilysiren/cli-mcp
[cli-web-docs]: https://github.com/coilysiren/cli-web-docs
[cli-web-ops]: https://github.com/coilysiren/cli-web-ops
[new-issue]: https://github.com/coilysiren/agent-guard/issues/new/choose

## See also

- [AGENTS.md](AGENTS.md) - agent-facing operating rules.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.agent-guard/agent-guard.yaml](.agent-guard/agent-guard.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
