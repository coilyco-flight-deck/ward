---
doc_goal: Define restart reconciliation and at-most-one worker start for durable broker dispatches.
---
# ward agent dispatch recovery

Before the broker reports healthy, it reconciles every nonterminal journal that
belongs to its Compose project against Docker containers carrying the
`ward.dispatch-request-id` label.

## Decisions

- A request interrupted before Docker created a container can replay because no
  worker start occurred.
- A stopped `created` container is adopted by ID, its copied launch inputs are
  refreshed, and that same container is started.
- A running, restarting, or paused container is adopted without another start.
- An exited or dead container is terminalized as failed.
- A journal that says start began but has no matching container is ambiguous.
  The broker records an interrupted terminal result and refuses blind replay.
- More than one matching container is an invariant violation. Broker readiness
  fails instead of choosing one.

This is at-most-one container start per request ID. It does not promise that an
already-started harness resumes after the worker container itself exits.

## See also

- [agent-dispatch-broker.md](agent-dispatch-broker.md) - transport, supervision,
  and launch milestones.
- [agent-reservation.md](agent-reservation.md) - cross-host issue ownership.
