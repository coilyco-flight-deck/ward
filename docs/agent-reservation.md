---
doc_goal: Keep the reservation anchor stable after the old page was collapsed.
---
# agent reservation

This page is the durable anchor for launch-time reservations.

- It covers the local sentinel and the remote marker comment.
- It keeps the TTL and release-marker comments readable.
- A launch intent is not a running engineer. Running capacity belongs to the
  visible container, while the launch intent is just the prelaunch lease.

## Release semantics

- A release-marker comment at or after the latest reservation retracts it.
- A terminal `WARD-OUTCOME` comment (done, submitted, merge-ready, blocked,
  failed) retracts it the same way, the moment it posts ([ward#1149](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1149)). A
  review-driven follow-up dispatched right after a run reports out no longer
  collides with the finishing run's hold or races the reaper's release comment.
- The reaper still posts the terminal release comment for legibility, but skips
  it when a newer reservation shows a follow-up run already took the issue over.
- A launch that never becomes visible releases its launch-intent sentinel
  immediately. The TTL is only the orphaned-launch backstop.

## Collisions and the redispatch marker

- A forwarded dispatch that still collides with a live hold defers: it posts a
  needs-redispatch marker without releasing the hold (the hold belongs to the
  run that is still working) and starts nothing.
- The needs-redispatch marker has an owner: the `ward agent director` heartbeat
  sweeps it and re-queues the issue, bounded by the redispatch attempt cap
  ([ward#1149](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1149)). See [agent-director-dispatch.md](agent-director-dispatch.md).

## See also

- [agent-lifecycle.md](agent-lifecycle.md) - the launch path.
- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
