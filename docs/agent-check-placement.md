---
doc_goal: Define the launch-check placement matrix so broker-time and pre-flight gates stay aligned when queued work drifts.
---
# ward agent check placement matrix

This page names the current placement of the launch guards that matter to a queued dispatch.

`broker-time` here means the request has reached the independently supervised
dispatch service.
`pre-flight` here means the launch is still being checked before the detached container starts real work.

## Matrix

- `broker-only`
  - request shape and routing: parse the role, action, argv, token, and transport before the broker forwards anything.
- `pre-flight-only`
  - owner trust gate.
  - issue closed / wrong-repo refusal.
  - host one-shot GO / NO-GO read.
  - launch-adjacent work such as image pull.
- `both`
  - reservation conflict / stale hold detection.
  - open-PR backpressure.
  - branch-state / continuation-branch checks for already-carried work.
  - repo engineer capacity and the global engineer pool ceiling.

## Why the duplicated rows exist

The duplicated rows are the driftable ones. A run can sit in a queue long enough for another launch to claim the reservation, a PR threshold to move, the branch to turn from fresh work into continuation work, or capacity to disappear. Re-reading those guards at launch time is defense against queue-time drift, not redundant noise.

## See also

- [agent-lifecycle.md](agent-lifecycle.md) - the launch path that consumes the matrix.
- [agent-preflight.md](agent-preflight.md) - the pre-flight surface.
- [agent-dispatch-broker.md](agent-dispatch-broker.md) - the brokered launch contract.
