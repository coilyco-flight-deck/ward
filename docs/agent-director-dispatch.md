---
doc_goal: Keep the director-dispatch anchor stable after the old page was collapsed.
---
# agent director dispatch

This page is the durable anchor for the director's dispatch tick.

- It covers the heartbeat's dispatch decision and hold logic.
- It keeps fresh reservation holds parked while stale ones re-enter the queue.
- It keeps the dispatcher comments readable after the old page was removed.

## The needs-redispatch sweep

- The refresh tick sweeps parked entries (submitted, merge-ready, blocked,
  failed) whose newest thread signal is an unhandled needs-redispatch marker -
  a failed or reservation-collision forwarded dispatch, or a pre-launch death -
  and re-queues them ([ward#1149](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1149)).
- The sweep is bounded by the redispatch attempt cap. At the cap the entry
  parks blocked instead of looping.
- An entry with a live engineer container, or whose newest signal is a run
  outcome, is left alone - the marker was already handled.

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-reservation.md](agent-reservation.md) - holds, releases, the marker.
- [agent-workflow.md](agent-workflow.md) - landing policy and review.
