---
doc_goal: Keep the stop-anchor stable after the old page was collapsed.
---
# agent stop

This page is the durable anchor for the targeted stop surface.

- It stops one visible running engineer through the dispatch broker.
- It is the deliberate counterpart to the idle reap path.
- It refuses ghost launch records and points the operator at `ward agent reap`
  or the stale-reservation cleanup path instead.
- `--print` uses the same stoppability check as `ward agent stop`.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [agent-director.md](agent-director.md) - the read-only director lane.
