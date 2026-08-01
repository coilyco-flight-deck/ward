# ward features

Inventory of what `ward` ships today.

## Core gate

- `ward exec` - gated repo verbs, including validated detached Forgejo CI merges.
- `ward audit` - append-only trail.
- `ward git` - audited git.
- `ward setup` - local bootstrap + policy check.
- `ward doctor` - config validation.
- `source-doc-refs` - source-comment doc-path validation.
- [`.ward/ward.yaml` schema](ward-yaml.md) and [Windows tests](windows-development.md).

## Agent surface

- **`ward agent`** - the guarded execution layer.
- **`warded`** - the symlinked public face.
- Typed harness adapters and three fixed workflow labels. Role metadata grants
  no authority. Preferences use explicit inputs or YAML.
- `ward agent director queue` / `status` - read-only queue view.
- Read-only Forgejo issue-comment guard.
- Reservation and dispatch comments clean up after release.
- Harness install hooks for claude, codex, goose, and opencode.
- Read-only context bundles with role and agent binding, home projection, and
  no authority fields. See [context-bundle.md](context-bundle.md).
- Tracker and forge adapters do not depend on AOSguard.
- Launch-intent vs running-engineer split in list, dispatch-health, reap, and director.
- Issue-thread-backed reservations with disposable cache and `ward agent reservations clear`.
- Open-PR backpressure gate.
- Issue-scoped director dispatch.
- Bounded director, engineer, and QA live-verification fixtures. See
  [verification-fixtures.md](verification-fixtures.md).
- Compose dispatch broker with durable IDs, restart recovery, sibling launch
  isolation, and credential-free director Forgejo RPC. See
  [agent-dispatch-broker.md](agent-dispatch-broker.md).
- Generic read-only peers and authenticated broker messages. See
  [agent-peer-collaboration.md](agent-peer-collaboration.md).
- PR-workflow tools with fixed workflow gates. See [agent-pr-workflow.md](agent-pr-workflow.md).
- PR lifecycle close/reopen/recovery tools and repair classification.
- Claude tool-failure buffer and secret-free drained skill-use summaries. See [agent-observability.md](agent-observability.md).
- Director defaults read-only; autonomous drain needs `--burndown` / `--drain`.
- `ward agent issue create` files a Forgejo issue through the read-only
  director credential broker without dispatching an engineer.
- Dispatch-health, PR repair input, and logs artifact selector.

## Container surface

- The ephemeral run box - see [container.md](container.md),
  [container-contract.md](container-contract.md),
  [container-lifecycle.md](container-lifecycle.md), and
  [container-substrate.md](container-substrate.md).
- Claude in Chrome browser computer-use is disabled for Claude Code containers.
- Optional context bundles stay authority-free. Ward retains credentials,
  permissions, mounts, network, and launch authority.
- Public demo image build. See [demo-image.md](demo-image.md).

## AOS policy and AOSguard boundary

- Ward owns typed launch mechanics without role profiles or runtime bundles.
- AOSguard owns generated operator APIs in AOS. Ward does not ship generated operator leaves.

## Release and docs

- Two-stage release ([ward#1117](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1117)): promote.yml gates every main push and
  fast-forwards `release` when green; release.yml runs on `release` pushes
  under a no-cancel concurrency queue. See [release.md](release.md).
- [compat-surface.md](compat-surface.md) - the release-facing provider matrix.
- [release.md](release.md) and [release-binaries.md](release-binaries.md).
- [homebrew-build.md](homebrew-build.md), [golangci.md](golangci.md), and [troubleshooting.md](troubleshooting.md).
- [docs/README.md](README.md) - docs index.

## See also

- [../README.md](../README.md) - front page
- [../AGENTS.md](../AGENTS.md) - operating rules.
- [features-release-tooling.md](features-release-tooling.md) - release tooling.
- [../.ward/ward.yaml](../.ward/ward.yaml) - repo allowlist.
