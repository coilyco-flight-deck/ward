---
doc_goal: Keep the downgrade-guard anchor stable after the old page was collapsed.
---
# agent ward downgrade

This page is the durable anchor for the ward-version downgrade guard.

- It refuses to launch a container pinned to an older ward binary.
- It exists because the in-container reaper is part of the safety boundary.
- The explicit override is a conscious opt-in, not a default path.

## See also

- [container-lifecycle.md](container-lifecycle.md) - launch, debug, teardown.
- [agent-lifecycle.md](agent-lifecycle.md) - the launch path.
