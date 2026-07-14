---
doc_goal: Keep the list-anchor stable after the old page was collapsed.
---
# agent list

This page is the durable anchor for the live engineer list surface.

- It shows which running engineers are active right now, and it also shows launch intents before their container appears. Capacity counts active launches only. Stale launch intents stay visible as cleanup-needed records, and failed-before-start records stay visible but do not count.
- It shows the live execution budget for each running role when that role has a limit.
- It is part of the operator run-surfaces set, not the launch path.
- It pairs naturally with logs and stop.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [troubleshooting.md](troubleshooting.md) - what to inspect when a run is stuck.
