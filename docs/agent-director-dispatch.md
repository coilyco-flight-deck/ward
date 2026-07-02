# Director dispatch-error disposition

How `ward agent director` classifies a **dispatch error** - the error the engineer command
returns before a container detaches - decides whether the issue is retried or parked. See
[agent-director.md](agent-director.md) for the heartbeat this feeds.

## The rule (ward#352, ward#524)

`directorDispatchDisposition` splits errors by whether they judged the **issue itself**:

- **Coded per-issue decline** - a pre-flight **NO-GO**, a **wrong-repo** bounce, or an
  **untrusted-owner** refusal (a `dispatchDeclineErr` / `exitcode.Coded` value). This is a
  real verdict that a retry cannot change, so the issue parks terminal `failed` with outcome
  status `declined`.
- **Everything else defers** - left `queued`, retried on a later tick:
  - a **reservation conflict** (another run holds the 2h-TTL reservation), retryable once the
    holder finishes (`--force` reclaims a stale/foreign hold);
  - a **launch-time infrastructure failure** - the pre-flight issue fetch, a network blip, a
    container bring-up error. These never judged the issue and consumed no autonomous run.

### Why launch/infra errors must not park

The pre-flight issue fetch (`resolveAgentWork` -> `fetchIssue`) runs on the host before any
container starts. A systemic failure there (a sour forge read path) would otherwise park
every dispatched issue `failed` one by one. That terminal-`failed` streak is exactly the
signal the director LLM reads to "hold this tick", so a transient outage wedged the whole
backlog permanently - nothing re-tested the recovered path. Deferring self-heals: the moment
the fetch path is healthy again, the next tick dispatches.

## The self-heal migration (ward#527)

The ward#524 rule is forward-looking - it does not un-strand issues the **old** classifier
already parked. On refresh, `applyRankedBacklogEntry` re-queues any headless-lane entry that
is `failed` with the legacy `dispatch-error` status (`isStrandedDispatchError`), clearing its
outcome. Because ward#524 gives genuine declines the distinct `declined` status, a
`dispatch-error` entry can only be a pre-ward#524 launch/infra stranding, so this recovers
exactly those without ever re-queuing a real per-issue decline.
