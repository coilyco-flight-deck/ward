---
doc_goal: Document the cache-only reservation directory and the supported whole-folder recovery path.
---
# reservation cache cleanup

The reservation directory is disposable cache.

- Path: `~/.ward/agent-reservations`.
- Safe emergency recovery: clear the whole directory, not one JSON or lock at a time.
- Ward recreates the directory on demand, so deleting it does not lose canonical issue-thread state.

## Supported cleanup

Use:

```bash
ward agent reservations clear
```

That command removes the reservation cache directory wholesale and recreates it empty.

## Manual fallback

If you must intervene by hand, delete `~/.ward/agent-reservations` wholesale.

- That is safe because the directory is cache-only.
- Do not move canonical reservation authority into that directory.
- Do not rely on file names for recovery.

## See also

- [agent-ops.md](agent-ops.md) - the operational surfaces.
- [troubleshooting.md](troubleshooting.md) - stale-launch symptoms and recovery.
