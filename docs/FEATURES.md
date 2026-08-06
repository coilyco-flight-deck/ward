# Ward capabilities

Inventory of the major capabilities Ward ships today.

## Governed repository commands and audit

* `ward exec` runs repository-declared commands through validation and records
  every invocation. `ward git` applies the same governed boundary to version
  control operations. See [repository commands](exec-verb.md),
  [configuration](ward-yaml.md), and [audit](audit.md).

## Isolated least-access agent execution

* Each run receives an ephemeral workspace, a private harness home, only its
  required credentials and mounts, and teardown tied to the launch contract.
  See [container overview](container.md) and
  [container contract](container-contract.md).

## Role selection and fixed landing workflows

* Fixed roles select execution behavior without granting authority. Fixed
  workflows determine whether work lands directly, through a review-gated
  change request, or on a remote branch. See [roles](agent-roles.md) and
  [landing workflows](agent-workflow.md).

## Durable dispatch, capacity, reservation, and restart recovery

* Issue-backed reservations, launch capacity, backpressure, durable request
  identities, and restart reconciliation keep detached dispatch observable and
  recoverable. See [agent lifecycle](agent-lifecycle.md) and
  [dispatch lifecycle](agent-dispatch-lifecycle.md).

## Read-only supervision, lifecycle status, logs, and cleanup

* Read-only supervision exposes queue and run status, secret-safe logs, and
  scoped stop and cleanup operations without granting repository mutation.
  See [agent operations](agent-ops.md), [director](agent-director.md), and
  [observability](agent-observability.md).

## Revision-bound independent verification

* Independent verification inspects an exact candidate revision and records a
  structured verdict. A later landing gate accepts the verdict only while it
  still matches the current candidate. See [QA verification](agent-qa.md) and
  [landing workflows](agent-workflow.md).

## Authority-free context handoff and brokered collaboration

* Validated context bundles carry selected instructions and tools without
  credentials or permissions. Brokered peers add authenticated messaging and
  durable identities without making role names authoritative. See
  [context bundles](context-bundle.md) and
  [brokered collaboration](agent-peer-collaboration.md).

## See also

* [README](../README.md) - product and adopter boundary.
* [AGENTS](../AGENTS.md) - repository operating rules.
* [repository configuration](../.ward/ward.yaml) - governed command allowlist.
* [catalog convention](features-release-tooling.md) - required catalog links.
* [compatibility matrix](compat-surface.md) - provider support and limitations.
