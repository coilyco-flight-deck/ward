---
doc_goal: Keep the cleanup anchor stable after the old page was collapsed.
---
# container cleanup

This page is the durable anchor for post-run cleanup.

- It describes the release of stopped containers and stale state.
- It sits beside the launch and teardown path, not the repo gate.
- It is the note for the cleanup sweep that follows a run.
- Stopped ward containers are reclaimed after the retention TTL (48h by default).

## See also

- [container-lifecycle.md](container-lifecycle.md) - launch, debug, teardown.
- [troubleshooting.md](troubleshooting.md) - symptoms after a run ends.
