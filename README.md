# ward

**ward is a harness driver.** It drives an agent harness (claude, goose, codex, qwen) into an ephemeral container to carry a Forgejo issue end to end, then gates every dev verb it runs behind [cli-guard][cli-guard]'s policy.

The thing it produces is a **warded agent**: an agent ward drives into a container and gates behind cli-guard policy. Read "warded" as a protective circle around the agent - the lockdown deny-list and allowlisted verbs bounding its reach, not "warded off".

Its public face is **`warded`** - a thin symlink onto the `ward drive <harness>` machinery (ward#247): `warded claude "..."` reads like `sudo`/`firejail`, one token for "containment tool for agents". See [`docs/drive.md`](docs/drive.md).

`ward` is the contributor-facing cli-guard consumer, now also absorbing the operator surface from the retiring [coily][coily] (spec REST on `ward-kdl`).

## Status

v0.x. Downstream consumers upgrade to the `ward` binary and `.ward` config on their own schedule.

## What it does

Wraps a project's dev verbs (`build`, `test`, `vet`, `lint`, `tidy`, `cover`) behind cli-guard's policy gate. Every invocation validates argv, writes one append-only JSONL audit row, and gates repo verbs on a clean+synced tree (see [`docs/exec-verb.md`](docs/exec-verb.md)).

Each repo declares which Makefile targets are exposed in `.ward/ward.yaml`. The contract is verified by `ward lint`.

## Install

Install from the centralized flight-deck tap:

```
brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap
brew install coilyco-flight-deck/tap/ward
```

The explicit-URL form is required because the tap lives on forgejo, not github.com. The formula installs `ward` and the spec-driven `ward-kdl` (both stamped with the release tag) plus the `warded` symlink. Upgrade with `ward upgrade`.

## Usage

```
ward exec build
ward exec test
ward lint
ward pkg brew bundle    # audited brew wrapper
```

See [`docs/FEATURES.md`](docs/FEATURES.md) for the full verb list.

## Claude Code PreToolUse hook

`ward hook pre-tool-use` is a stdin-driven [Claude Code hook](https://docs.claude.com/en/docs/claude-code/hooks). It does two things:

1. **Binary-path check.** Refuses `ward`/`coily` unless `command -v` resolves to a canonical homebrew path, blocking PATH-hijack. On by default.
2. **Routing-hint surface.** Catches bare wrapped binaries (`make`, `gh`, `aws`, ...) and names the right wrapper; the active table is picked by `.ward/ward.yaml` vs `.coily/coily.yaml`.

No network, no state. Failures pass through silently. Hard denial stays the job of `permissions.deny`. Register with `ward install-hooks` (idempotent), which writes the PreToolUse entry to `<git-toplevel>/.claude/settings.json`.

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
