# Container and substrate

What a run gets on disk, and which of it is authoritative writable work.

## Ward containers

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
* [container.md](container.md) - workspace and references.

## Workspace and substrate

The target clone is authoritative writable work. It starts at
`/workspace/<repository>`. Additional owner-qualified repositories use
`/workspace/<owner>/<repository>` so equal basenames can coexist. A repository
is writable only when the workflow explicitly grants it.

`/substrate` contains product-provided read-only reference checkouts. Optional
context-bundle repositories mount separately at
`/refs/<owner>/<repository>`. Both are reference inputs, never landing targets.

The same repository may appear as the writable target and as a read-only
reference snapshot. Once work changes, the reference is stale by design. Read
from either when useful and act only in the granted workspace.

The reaper verifies the selected landing target and ignores read-only
substrate or context references when deciding whether work landed.

## See also

* [container-contract.md](container-contract.md) - mounts and permissions.
* [context-bundle.md](context-bundle.md) - `/refs` validation.
* [container-lifecycle.md](container-lifecycle.md) - landing and teardown.
