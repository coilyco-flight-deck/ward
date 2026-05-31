# ward

A contributor-facing [cli-guard][cli-guard] consumer. `ward` sits between AI agents (or any semi-trusted automation) and the host system when working inside a ward-managed repo, wrapping a project's dev verbs behind cli-guard's policy gate.

`ward` is the contributor counterpart to [coily][coily]. coily is the operator CLI - personal machines, homelab SSH, vault paths, deploy hooks. `ward` is the gate a contributor (human or agent) routes through to build, test, and lint project code. Both are thin, audited wrappers around the same cli-guard primitives, split by who is driving: operator vs contributor.

## Status

v0. Downstream consumers upgrade to the `ward` binary and `.ward` config on their own schedule.

## What it does

Wraps a project's dev verbs (`build`, `test`, `vet`, `lint`, `tidy`, `cover`) behind cli-guard's policy gate. Every invocation validates argv, writes one append-only JSONL audit row, binds to a git toplevel via `--commit-scope`, and refuses repo-shaped verbs on a dirty tree.

Each repo declares which Makefile targets are exposed in `.ward/ward.yaml`. The contract is verified by `ward lint`.

## Install

```
brew tap coilyco-flight-deck/ward https://forgejo.coilysiren.me/coilyco-flight-deck/ward
brew install coilyco-flight-deck/ward/ward
```

The explicit-URL `brew tap` form is required because this repo isn't `homebrew-*` prefixed. The installed binary is `ward`.

## Usage

```
ward exec build
ward exec test
ward lint
```

See [`docs/`](docs/) for the full verb list.

## Claude Code PreToolUse hook

`ward hook pre-tool-use` is a stdin-driven [Claude Code hook](https://docs.claude.com/en/docs/claude-code/hooks). It does two things:

1. **Binary-path check.** Refuses to let `ward` or `coily` run unless `command -v` resolves to a canonical homebrew install path. Blocks PATH-hijack attacks. On by default, no flag.
2. **Routing-hint surface.** Catches bare invocations of wrapped binaries (`make`, `gh`, `aws`, `kubectl`, ...) and surfaces a recovery hint naming the right wrapper. The active table is picked by `.ward/ward.yaml` vs `.coily/coily.yaml` in cwd.

No network, no state. Failure modes pass through silently. Hard denial stays the job of `permissions.deny`.

Register with `ward install-hooks` (idempotent). Writes the PreToolUse entry to `<git-toplevel>/.claude/settings.json`. Flags: `--path <file>`, `--dry-run`, `--check`.

## Related

- [cli-guard][cli-guard] - underlying security-boundary framework
- [coily][coily] - the operator-facing cli-guard consumer
- [cli-mcp][cli-mcp] - sibling cli-guard consumer that projects a urfave/cli tree as an MCP server

## Support

Bug or feature request: [create a new issue][new-issue]. Conduct: [Code of Conduct](CODE_OF_CONDUCT.md). Security: [SECURITY.md](SECURITY.md). License: [`LICENSE`](./LICENSE).

[cli-guard]: https://forgejo.coilysiren.me/coilyco-flight-deck/cli-guard
[coily]: https://github.com/coilyco-bridge/coily
[cli-mcp]: https://github.com/coilysiren/cli-mcp
[new-issue]: https://github.com/coilyco-flight-deck/ward/issues/new/choose

## See also

- [AGENTS.md](AGENTS.md) - agent-facing operating rules.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
