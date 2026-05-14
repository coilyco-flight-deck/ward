# agent-guard

A generic-purpose [cli-guard][cli-guard] consumer for repos that take external contributions. Sits between AI agents (or any semi-trusted automation) and the host system, with no maintainer-specific allowlists.

`agent-guard` is to external contributors what [coily][coily] is to Kai's own machines: a thin, audited wrapper around the cli-guard primitives. coily ships personal verbs (homelab SSH, vault paths, deploy hooks). `agent-guard` ships only verbs that make sense to any contributor walking up to a repo cold.

## Status

v0. Not yet wired into any downstream. First adopter target is the urfave/cli namespaced repos ([cli-mcp][cli-mcp], [cli-web-docs][cli-web-docs], [cli-web-ops][cli-web-ops]).

## What it does

Wraps a small, fixed set of dev verbs (`build`, `test`, `vet`, `lint`, `tidy`) behind cli-guard's policy gate. Every invocation:

- validates argv against shell-metacharacter rejection
- writes one append-only JSONL audit row
- binds to a git toplevel via `--commit-scope`
- refuses repo-shaped verbs on a dirty tree

Downstream repos add an `.agent-guard/agent-guard.yaml` listing which Makefile targets are exposed. The contract is verified by `agent-guard lint`.

## Install

Build from source until releases ship:

```
go install github.com/coilysiren/agent-guard/cmd/agent-guard@latest
```

## Usage

```
agent-guard exec build
agent-guard exec test
agent-guard lint
```

See [`docs/`](docs/) for the full verb list and [`examples/`](examples/) for runnable demos.

## Claude Code PreToolUse hook

`agent-guard hook pre-tool-use` is a stdin-driven [Claude Code hook](https://docs.claude.com/en/docs/claude-code/hooks) that catches bare invocations of wrapped binaries (`make`, `gh`, `aws`, `kubectl`, ...) and surfaces a recovery hint to the agent before it shops other shell shapes. It detects whether cwd lives under `.agent-guard/agent-guard.yaml` or `.coily/coily.yaml` and routes the hint to the matching wrapper.

No network, no state. Failure modes (unparseable payload, missing fields, no matching route) pass through silently. Hard denial stays the job of `permissions.deny` in the consuming repo's `.claude/settings.json`.

Register the hook with one command (idempotent, safe to re-run, preserves unrelated keys):

```
agent-guard install-hooks
```

This writes the PreToolUse entry into `<git-toplevel>/.claude/settings.json`. Pass `--path <file>` to target a different settings.json, `--dry-run` to preview the merged content, or `--check` (exit non-zero when the hook is not yet registered, for CI).

Or hand-roll the entry:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "agent-guard hook pre-tool-use" }
        ]
      }
    ]
  }
}
```

## Related

- [cli-guard][cli-guard] - the underlying security-boundary framework
- [coily][coily] - Kai's personal cli-guard consumer
- Sibling cli-* repos: [cli-mcp][cli-mcp], [cli-web-docs][cli-web-docs], [cli-web-ops][cli-web-ops]

## Support

If you found a bug or have a feature request, [create a new issue][new-issue]. Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md). Security disclosures go through [SECURITY.md](SECURITY.md).

### License

See [`LICENSE`](./LICENSE).

[cli-guard]: https://github.com/coilysiren/cli-guard
[coily]: https://github.com/coilysiren/coily
[cli-mcp]: https://github.com/coilysiren/cli-mcp
[cli-web-docs]: https://github.com/coilysiren/cli-web-docs
[cli-web-ops]: https://github.com/coilysiren/cli-web-ops
[new-issue]: https://github.com/coilysiren/agent-guard/issues/new/choose
