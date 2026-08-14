---
doc_goal: Define Ward's source-aligned product, process, authority, and persistence boundaries.
---
# Ward architecture

Ward is one CLI with two execution paths and one shared audit posture.

## Repository command path

`ward exec`, `ward git`, and `ward audit` are native command surfaces built on
the `umbra` policy and routing engine. Ward discovers `.ward/ward.yaml`,
validates declared argv and repository state, executes the admitted command,
and records the result. On Linux, admitted commands can run in the sandbox
jail. macOS and Windows enforce the declared boundary at command entry.

## Agent execution path

`ward agent` and its `warded` symlink resolve a fixed role, typed harness
adapter, landing workflow, launch checks, and ephemeral container plan. Direct
host launches and brokered launches share the same reservation, capacity,
credential, container, artifact, and workflow contracts.

## Authority boundaries

* Roles and context bundles describe intent. They grant no credentials,
  mounts, network, broker operations, or merge authority.
* The host owns config discovery, credential resolution, launch staging, and
  container creation.
* The broker owns durable forwarded requests and typed tracker or pull-request
  operations. It never returns its broad credential.
* The container owns one live workspace and harness process. The target
  checkout is writable. Reference inputs are read-only.
* The tracker thread owns durable work identity and reservation authority.
* The selected workflow defines evidence that work landed.

## Persistence boundaries

Containers are disposable. Issue comments, remote Git refs, pull requests,
audit JSONL, secret-safe drained artifacts, dispatch journals, and verified
Git rescue bundles may outlive a run. Local reservation files and Docker state
are caches, not tracker authority.

Provider-specific operator automation is outside Ward. Ward ships typed
adapters for the provider surfaces listed in [compat-surface.md](compat-surface.md).

## See also

* [exec-verb.md](exec-verb.md) - repository command enforcement.
* [agent-lifecycle.md](agent-lifecycle.md) - agent launch sequence.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - broker authority.
* [container-contract.md](container-contract.md) - host/container boundary.
