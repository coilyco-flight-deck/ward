# agent preflight

The preflight is the launch-time go/no-go check.

- It runs before the container starts real work.
- It checks the target, the harness, and the repo context.
- It can stop a launch before any implementation work begins.
- It re-checks the queue-drift guards named in [agent-check-placement.md](agent-check-placement.md), because reservations, PR pressure, branch state, and capacity can change while work waits.

## Bypasses

- `--skip-smoke-test` skips only the in-container harness smoke test. The host preflight, launch-adjacent probes, capacity checks, and review gate still run.
- `WARD_SMOKE_TEST_SKIP=1` is the supported environment alias for direct launches. A brokered engineer launch normalizes the environment setting into the visible `--skip-smoke-test` argument before forwarding it.
- `--skip-preflight` is broader. It also bypasses the host preflight, reservation re-check wait, launch-adjacent probes, and review gate.

## See also

- [agent-lifecycle.md](agent-lifecycle.md) - the full launch path.
- [agent-trust-gate.md](agent-trust-gate.md) - the owner gate.
