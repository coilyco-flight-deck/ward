---
doc_goal: Inventory Ward's major shipped capabilities without implementation history or provider branding.
---
# Ward capabilities

## Governed repository commands and audit

* `ward exec` runs declared commands through argument validation and a single
  repository-state gate: the declaring `.ward/ward.yaml` must be committed. It
  contacts no remote, so it works offline. `ward git` governs supported
  version-control operations. Both write audit records carrying the resolved
  argv. See [commands](exec-verb.md), [repository configuration](ward-yaml.md),
  and [audit](audit.md).

## Isolated least-access execution

* Agent runs receive an ephemeral workspace, private harness home, scoped
  credentials and mounts, and verified teardown. See [container overview](container.md)
  and [container contract](container-contract.md).

## Fixed roles and landing workflows

* Fixed roles select workflow behavior without granting authority. Fixed
  workflows decide whether work lands directly, through a change request, or
  on a remote branch. See [roles](agent-roles.md) and [workflows](agent-workflow.md).

## Durable dispatch and recovery

* Issue-backed reservations, capacity gates, durable request identities, and
  restart reconciliation make detached dispatch observable and recoverable.
  See [lifecycle](agent-lifecycle.md) and [broker](agent-dispatch-broker.md).

## Read-only supervision and operations

* Read-only supervision exposes live queue state, secret-safe logs, lifecycle
  status, and scoped stop and cleanup operations. See [director](agent-director.md),
  [operations](agent-ops.md), and [observability](agent-observability.md).

## Revision-bound verification

* Independent QA records a structured verdict for one exact candidate
  revision. Landing accepts it only while that revision remains current. See
  [roles](agent-roles.md) and [workflows](agent-workflow.md).

## Authority-free context and collaboration

* Validated context bundles carry selected instructions and tools without
  permissions. Brokered peers add authenticated messaging and durable
  identities without making role names authoritative. A typed release handoff
  carries an immutable candidate between Director and one Ops peer. Ward then
  serializes deploy-state mutation, typed verification, Git CAS publication,
  and recovery without embedding provider semantics or granting operation
  authority. See [context bundles](context-bundle.md),
  [collaboration](agent-peer-collaboration.md), and
  [release contract](agent-release.md).

## See also

* [README](../README.md) - product and adopter boundary.
* [AGENTS](../AGENTS.md) - repository operating rules.
* [repository configuration](../.ward/ward.yaml) - governed command allowlist.
* [compatibility matrix](compat-surface.md) - provider support and limitations.
