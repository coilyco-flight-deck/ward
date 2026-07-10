---
doc_goal: Describe the read-only substrate and multi-repo mounts as one operator-facing contract instead of a set of issue slices.
---
# ward container substrate

`/substrate` holds read-only reference checkouts that every container session
can read.

- It is a reference cache, not a work surface.
- The workspace clone remains the authoritative writable tree.
- Multi-repo runs can add extra read-only grants or repo mounts.

## What to expect

- The container sees the target repo first.
- Dependencies or granted repos can appear read-only beside it.
- The reaper ignores read-only references when it verifies landing.

## Why it exists

- it keeps shared dependencies out of the writable workspace.
- it lets the run inspect upstreams without mutating them.
- it gives the reaper a clean line between landing work and borrowing context.

## Multi-repo behavior

The same container model can mount extra repos in read-only mode for larger
flows. The rule stays the same: only the target repo is writable unless the
workflow explicitly granted something else.

## See also

- [container-lifecycle.md](container-lifecycle.md) - how the box starts and ends.
- [container-contract.md](container-contract.md) - the mount and env contract.
