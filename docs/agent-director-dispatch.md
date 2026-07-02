# Director dispatch-error disposition

How `ward agent director` classifies a **dispatch error** - the error the engineer command
returns before a container detaches - decides whether the issue is retried or parked. See
[agent-director.md](agent-director.md) for the heartbeat this feeds.

## The rule

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

## Closing the LLM-layer livelock

Deferring self-heals the **mechanical** layer, but only on the next *attempt*. The **LLM
judgment** layer on top could still livelock: an infra-failure streak from before [ward#524](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/524) (or a run
that parked `failed` with a 502 in its outcome text) stays the newest signal in RECENT
OUTCOMES, the decision holds on it, holding dispatches nothing, and no fresh outcome ever
displaces the stale one - a permanent `DISPATCH: none` hold with slots free and issues queued.

Two levers break it, both in the heartbeat ([agent-director.md](agent-director.md)):

- **Live forge-health probe** - each schedulable tick runs one cheap `issue get` on the top
  candidate (the exact read the pre-flight fails on) and feeds `FORGE HEALTH: ok | degraded |
  unknown` into the decision prompt, telling the LLM a recovered forge makes a past
  infra-failure streak stale.
- **Livelock guard** - a deterministic backstop: when the decision holds anyway but the probe
  is `ok` AND the only failing signal in RECENT OUTCOMES is infrastructure (`isInfraFailureOutcome`
  - the 5xx / bad-gateway / dispatch-error / failing-issue-fetch fingerprints), it force-dispatches
  the single top candidate. The defer path above makes that probe-dispatch cheap and safe, so it
  re-tests the recovered forge rather than trusting the LLM to. A substantive engineer
  block/failure in the window vetoes the override, and a non-`ok` probe leaves the hold intact.

## The self-heal migration

The [ward#524](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/524) rule is forward-looking - it does not un-strand issues the **old** classifier
already parked. On refresh, `applyRankedBacklogEntry` re-queues any headless-lane entry that
is `failed` with the legacy `dispatch-error` status (`isStrandedDispatchError`), clearing its
outcome. Because the rule gives genuine declines the distinct `declined` status, a
`dispatch-error` entry can only be a launch/infra stranding from before [ward#524](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/524), so this
recovers exactly those without ever re-queuing a real per-issue decline.
