# ward container reap provenance

`ward container reap` now records a small dispatch-time provenance file in the
target worktree. It carries the container/run id, the carried repo + issue,
the reservation timestamp, and the `origin/main` sha seen at dispatch.

The reaper reads that record back and only treats a run as done when it can
show a commit on `origin/main` newer than that baseline and newer than the
reservation. That blocks stale attribution from an already-landed commit that
predates the issue or the reservation window.

See also:

[docs/container-reap.md](container-reap.md) - teardown flow.
[docs/FEATURES.md](FEATURES.md) - inventory.
