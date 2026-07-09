---
doc_goal: Explain when a detached agent run's logs become observable - the exit-waiter that drains the moment a container exits versus the keep-10 sweep backstop - and why the design is safe (docker wait only observes) and idempotent (the shared sentinel), so an operator watching the log tree knows a just-closed run will not stay invisible.
---
# drain timing - shortly after exit

A `ward agent` run's console + transcript + `meta.json` are pulled **host-side**
(the [reaper](container-reap.md) runs inside the container with no docker socket).
Until [ward#510](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/510)
that pull happened **only** at keep-10 eviction, so a just-closed run stayed
invisible under `~/.ward/agent-logs/` until the next launch aged it out - too late
for the operator watching that tree
([infrastructure#424](https://forgejo.coilysiren.me/coilyco-flight-deck/infrastructure/issues/424)).
Now there are two drain points; the [observability](agent-observability.md) doc
covers what a drain pulls, this one covers **when**.

## The exit waiter - the earliest safe point

A detached run is fire-and-forget: nothing on the host watches its container. So at
launch, ward spawns a detached `ward container drain-exit <container>` grandchild
that blocks on `docker wait` and drains the run **the moment it exits**.

- `docker wait` only **observes** the exit, so a live run is never touched (the
  "no destructive interaction with live containers" safety property holds).
- The waiter is detached (its own session, released, no inherited stdio), so it
  outlives the launching `ward agent` process and never writes to the console.
- It is **best-effort**: a spawn failure only logs, then leaves the sweep to drain
  the run later - it never fails the launch.
- **In-container** dispatch skips the waiter: a grandchild spawned inside a
  container would die when that container is reaped, so an in-container launch leans
  on the next host sweep's drain instead.

## The keep-10 sweep - the backstop

Every launch's [stale-container sweep](container-cleanup.md) drains **every** exited
run it lists (not just the past-keep-10 tail it removes), then `docker rm`s that
tail. This covers whatever the waiter missed: an in-container dispatch, a spawn
failure, a run left by an older ward. Drain is still ordered before the `rm`
([ward#363](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/363)) - the `rm` takes the log with it.

## Idempotency - the shared marker

Both points route through `drainAgentRunIdempotent`. A completed drain writes a
zero-byte sentinel at `~/.ward/agent-logs/.drained/<container>`; a second drain sees
it and is a cheap no-op. So the waiter and the later sweep never double-pull the
same run, whichever reaches it first.

The sentinel is **cleared when the container is removed** - by the sweep's `docker
rm`, or by an engineer clearing an exited same-name corpse before relaunch. Engineer
container names are deterministic (`engineer-<driver>-<repo>-<N>`), so a re-run
reuses the name; clearing the dead run's marker on removal lets the re-run drain
fresh rather than being skipped by a stale sentinel. Issueless roles carry a machine
suffix and never collide.

## See also

- [agent-observability.md](agent-observability.md) - what a drain pulls, and the sinks.
- [container-cleanup.md](container-cleanup.md) - the keep-10 sweep.
- [container-reap.md](container-reap.md) - the in-container teardown reaper.
