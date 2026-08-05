# ward features

Inventory of what `ward` ships today.

## Core gate

- `ward exec` - gated repo verbs, including validated Forgejo CI pull-request merge checkouts.
- `ward audit` - append-only trail.
- `ward git` - audited git.
- `ward setup` - local bootstrap + policy check.
- `ward doctor` - config validation.
- `source-doc-refs` - source-comment doc-path validation.
- [`.ward/ward.yaml` schema](ward-yaml.md) and [Windows tests](windows-development.md).

## Agent surface

- **`ward agent`** - the guarded execution layer.
- **`warded`** - the symlinked public face.
- Typed harness adapters and fixed workflows. Role metadata grants no authority.
- `ward agent director queue` / `status` - read-only live queue view with a stable versioned JSON schema.
- Read-only Forgejo issue-comment guard.
- Reservation and dispatch comment cleanup.
- Harness install hooks for claude, codex, goose, and opencode.
- Authority-free context bundles and read-only `/refs` mounts. See [context-bundle.md](context-bundle.md).
- Tracker and forge adapters do not depend on AOSguard.
- Launch-intent vs running-engineer split in list, dispatch-health, reap, and director.
- Issue-thread-backed reservations with disposable cache and `ward agent reservations clear`.
- Open-PR backpressure gate.
- Issue-scoped director snapshot and attached read-only surface. Harness-native goals own repetition and dispatch judgment.
- Bounded engineer and QA fixtures. See [verification-fixtures.md](verification-fixtures.md).
- Compose broker with durable cluster/request IDs, restart recovery, sibling
  launch, lifecycle status, and Forgejo RPC. See [agent-dispatch-lifecycle.md](agent-dispatch-lifecycle.md).
- Broker-only clusters with scoped lifecycle verbs. See [agent-clusters.md](agent-clusters.md).
- Generic peers with broker IDs, messages, roster labels, and context plans. See
  [agent-peer-collaboration.md](agent-peer-collaboration.md).
- PR-workflow tools with fixed workflow gates. See [agent-pr-workflow.md](agent-pr-workflow.md).
- PR close/reopen/recovery and repair classification.
- Secret-safe agent archives with exact-value and RE2 redaction. See [agent-observability.md](agent-observability.md).
- Director reads one live startup snapshot and persists no orchestration ledger.
- `ward agent issue create` files through the director broker without dispatch.
- Actor admission seals exact external snapshots. Agent tracker writes use
  role-bound typed broker actions and Git-only credentials. See [agent-human-feedback.md](agent-human-feedback.md).
- Dispatch-health, PR repair input, and logs artifact selector.

## Container surface

- The ephemeral run box. See [container.md](container.md),
  [container-contract.md](container-contract.md), and [container-lifecycle.md](container-lifecycle.md).
- Claude in Chrome browser computer-use is disabled for Claude Code containers.
- Optional bundles stay authority-free. Ward retains credentials and launch authority.
- Public demo image build. See [demo-image.md](demo-image.md).

## AOS policy and AOSguard boundary

- Ward owns typed launch mechanics without role profiles or runtime bundles.
- AOSguard owns generated operator APIs in AOS. Ward does not ship generated operator leaves.

## Release and docs

- Two-stage gated promotion and release. See [release.md](release.md).
- [compat-surface.md](compat-surface.md) - the release-facing provider matrix.
- [release.md](release.md) and [release-binaries.md](release-binaries.md).
- [homebrew-build.md](homebrew-build.md), [golangci.md](golangci.md), and
  [troubleshooting.md](troubleshooting.md).

## See also

- [../README.md](../README.md) - front page
- [../AGENTS.md](../AGENTS.md) - operating rules.
- [features-release-tooling.md](features-release-tooling.md) - release tooling.
- [../.ward/ward.yaml](../.ward/ward.yaml) - repo allowlist.
