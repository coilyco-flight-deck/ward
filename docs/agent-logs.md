---
doc_goal: Keep the logs-anchor stable after the old page was collapsed.
---
# agent logs

This page is the durable anchor for reading one run's logs.

- It prefers the live container and falls back to the drained archive.
- It is the first read when a run looks stuck or silent.
- It is an operator surface, not a repo policy surface.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [troubleshooting.md](troubleshooting.md) - symptom-indexed recovery.
