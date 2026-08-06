---
doc_goal: Define the target workspace, owner-qualified multi-repo layout, read-only substrate, explicit grants, and reaper boundary.
---
# Workspace and substrate

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
