---
doc_goal: Keep the stop-anchor stable after the old page was collapsed.
---
# agent stop

This page is the durable anchor for the targeted stop surface.

- It stops one visible running engineer through the dispatch broker.
- For an issue ref, it clears a local launch record whose confirmation window
  elapsed without a running container, including the issue reservation marker.
- It is the deliberate counterpart to the idle reap path.
- It refuses a fresh launch intent until the confirmation window elapses.
- `--print` previews the stop or stale-record cleanup without changing state.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [agent-director.md](agent-director.md) - the read-only director lane.
