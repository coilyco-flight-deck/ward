---
doc_goal: Explain the ephemeral container model and route its durable host, lifecycle, and filesystem contracts.
---
# Ward containers

Ward gives each agent run a fresh clone and private harness home inside an
ephemeral least-access container. The host stages only the selected launch
assets, credential channel, read-only references, and writable runtime paths.

The target workspace is authoritative for work. Shared substrate and context
references are read-only. Engineer and QA receive a distinct Git-only
credential plus role-bound broker access. Director receives its harness
credential and broker capability without a transferable forge token.

Teardown is part of the launch contract. Ward drains secret-safe artifacts,
proves workflow landing, preserves unlanded committed Git history when needed,
and then reaps or retains explicit recovery state.

## See also

* [container-contract.md](container-contract.md) - mounts, env, credentials, and skills.
* [container-lifecycle.md](container-lifecycle.md) - launch through cleanup.
* [container-staging.md](container-staging.md) - host staging security.
* [container-substrate.md](container-substrate.md) - workspace and references.
