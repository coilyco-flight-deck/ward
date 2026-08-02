---
doc_goal: Keep the stop-anchor stable after the old page was collapsed.
---
# agent stop

This page is the durable anchor for the targeted stop surface.

- It stops one visible running engineer through the dispatch broker.
- It also accepts a broker-minted peer id. From a director or peer cluster it
  resolves that id inside the current cluster. From the host it refuses an
  ambiguous id rather than guessing across clusters.
- For an issue ref, it clears a local launch record whose confirmation window
  elapsed without a running container, including the issue reservation marker.
- It is the deliberate counterpart to the idle reap path.
- It refuses a fresh launch intent until the confirmation window elapses.
- `--print` previews the stop or stale-record cleanup without changing state.
- A peer stop marks its durable broker admission stopped, so it disappears
  from the active roster while its journal remains available for recovery.

```bash
ward agent stop critic-ab45
ward agent stop critic-ab45 --print
```

Issue and container-name targets remain engineer-only. A peer-id target must
match the `ward.peer` label on a generic collaboration peer. Director, QA,
session, and broker containers remain outside this surface.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [agent-director.md](agent-director.md) - the read-only director lane.
