# agent preflight

The preflight is the launch-time go/no-go check.

- It runs before the container starts real work.
- It checks the target, the harness, and the repo context.
- It can stop a launch before any implementation work begins.
- It re-checks the queue-drift guards named in [agent-check-placement.md](agent-check-placement.md), because reservations, PR pressure, branch state, and capacity can change while work waits.

## See also

- [agent-lifecycle.md](agent-lifecycle.md) - the full launch path.
- [agent-trust-gate.md](agent-trust-gate.md) - the owner gate.
