# ward

**ward is a harness driver.** It drives an agent harness (claude, goose, codex, qwen) into an ephemeral container to carry a Forgejo issue end to end, then gates every dev verb it (or any semi-trusted automation) runs behind [cli-guard][cli-guard]'s policy.

The thing it produces is a **warded agent**: an agent ward drives into a container and gates behind cli-guard policy. Read "warded" as a protective circle around the agent - deny-list and allowlisted verbs bounding its reach, not "warded off".

`ward` is the contributor-facing cli-guard consumer, now also absorbing the operator surface from the retiring [coily][coily] (spec REST on `ward-kdl`).

## Status

v0.x. Downstream consumers upgrade to the `ward` binary and `.ward` config on their own schedule.

## What it does

Wraps a project's dev verbs (`build`, `test`, `vet`, `lint`, `tidy`, `cover`) behind cli-guard's policy gate. This is the gate that makes a warded agent. Every invocation validates argv, writes one append-only JSONL audit row, stamps a best-effort `repo_root`, and gates repo verbs on a clean+synced tree (see [`docs/exec-verb.md`](docs/exec-verb.md)).

Each repo declares which Makefile targets are exposed in `.ward/ward.yaml`. The contract is verified by `ward lint`.

## Install

Install from the centralized flight-deck tap:

```
brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap
brew install coilyco-flight-deck/tap/ward
```

The explicit-URL `brew tap` form is required because the tap lives on forgejo, not github.com. The formula installs two binaries: `ward` and the spec-driven `ward-kdl`, both stamped with the release tag. Upgrade with `ward upgrade`.

## Usage

```
ward exec build
ward exec test
ward lint
ward pkg brew bundle    # audited brew wrapper (parity with coily pkg brew)
```

See [`docs/FEATURES.md`](docs/FEATURES.md) for the full verb list.

## Claude Code PreToolUse hook

`ward hook pre-tool-use` is a stdin-driven [Claude Code hook](https://docs.claude.com/en/docs/claude-code/hooks). It does two things:

1. **Binary-path check.** Refuses to let `ward` or `coily` run unless `command -v` resolves to a canonical homebrew install path. Blocks PATH-hijack attacks. On by default, no flag.
2. **Routing-hint surface.** Catches bare invocations of wrapped binaries (`make`, `gh`, `aws`, `kubectl`, ...) and surfaces a hint naming the right wrapper. The active table is picked by `.ward/ward.yaml` vs `.coily/coily.yaml` in cwd.

No network, no state. Failures pass through silently. Hard denial stays the job of `permissions.deny`.

Register with `ward install-hooks` (idempotent), which writes the PreToolUse entry to `<git-toplevel>/.claude/settings.json`. Flags: `--path`, `--dry-run`, `--check`.

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

- [docs/architecture.md](docs/architecture.md) - ward in three layers (cli-guard, ward-kdl, ward).
- [docs/comparison-openshell.md](docs/comparison-openshell.md) - ward vs NVIDIA OpenShell: verb-level gate vs kernel sandbox.
- [AGENTS.md](AGENTS.md) - agent-facing operating rules.
- [docs/FEATURES.md](docs/FEATURES.md) - inventory of what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - allowlisted commands.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
