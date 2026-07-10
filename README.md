# ward

`ward` wraps a repo's dev verbs behind a policy gate, so `make` and `go`
invocations stay argv-checked, audited, and tied to git history. The same
binary also ships `ward agent`, the guarded execution layer for coding agents
that runs in an ephemeral container and lands work through a selectable
workflow.

## Who it's for

- Contributors who want `build`, `test`, `lint`, and friends gated on a clean
  repo.
- Operators who want a short-lived agent container with a durable audit trail.

## What it requires

- macOS or Linux plus Homebrew for install.
- Docker for `ward agent`.
- A Forgejo instance for ward's own operator surface.

The plain verb gate needs only the repo and its `.ward/ward.yaml`.

## Install

```bash
brew tap coilyco-flight-deck/tap https://forgejo.coilysiren.me/coilyco-flight-deck/homebrew-tap
brew install coilyco-flight-deck/tap/ward
```

## Usage

```bash
ward exec test
ward git commit -m ...
ward audit tail --follow
ward agent engineer #98
warded #98
```

## What it does

- `ward exec` runs a repo dev verb through the gate.
- `ward git` exposes audited, concurrency-safe git.
- `ward agent` launches a coding agent in an ephemeral container.
- `ward kdl`-backed surfaces are embedded in the shipped binary.

## See also

- [AGENTS.md](AGENTS.md) - the agent operating rules.
- [docs/FEATURES.md](docs/FEATURES.md) - what ships today.
- [.ward/ward.yaml](.ward/ward.yaml) - the repo allowlist.
- [docs/README.md](docs/README.md) - the docs index.
- [docs/architecture.md](docs/architecture.md) - the three-layer model.

## Support

Canonical development happens on Forgejo. The GitHub mirror is the public
front door for external contributors.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
