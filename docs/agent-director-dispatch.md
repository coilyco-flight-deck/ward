---
doc_goal: Make a reader trust that the director retries transient launch/infra failures instead of parking issues terminally, and understand the forge-health probe and livelock guard that keep a recovered forge from wedging the backlog.
---
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

## A pre-launch death re-queues, it does not park ([ward#595](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/595))

`directorDispatchDisposition` above governs an error the engineer command returns **before**
the container detaches. A distinct death happens **after** detach: the container launches,
passes the reservation post, then dies at the [ward#222](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/222) smoke gate before the agent
ever runs - the reaper releases the reservation and the container is gone. The reconcile pass
(`backlogReconcile`) sees a `dispatched` entry with no running container and **no
WARD-OUTCOME**, and used to park it terminal `failed` ("exited-no-outcome"). That is the same
mistake the defer rule warns against, one layer over: a run that **consumed no autonomous
work** parked as if it had failed on the merits, and - worse - whether a retry happened was a
coin-flip on an incidental second attempt landing on a healthy host. When none did, the issue
was silently **orphaned** (open, reservation released, nothing running, nothing signalling).

`reconcileNoOutcome` closes that. When the thread carries a **reservation-release marker
stamped at/after the entry's dispatch** (`prelaunchDeathRelease` - the fingerprint of a
pre-launch death, distinct from a run that launched and vanished, which leaves no release),
the entry **re-queues** rather than parking `failed`: the next tick re-dispatches it
deterministically. This is bounded by `redispatchAttemptCap` (3) tracked on the entry's
`RedispatchAttempts`: a multi-host fleet lands a retry on a healthy host, while a
persistently-sick host exhausts the cap and parks the entry `blocked` with the
`orphaned-needs-redispatch` outcome (a loud, human-look terminal state), never a livelock.
A genuine no-outcome exit - the agent launched, did work, and vanished without a WARD-OUTCOME
- has no release marker and still parks `failed` as before.

## The self-heal migration

The [ward#524](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/524) rule is forward-looking - it does not un-strand issues the **old** classifier
already parked. On refresh, `applyRankedBacklogEntry` re-queues any headless-lane entry that
is `failed` with the legacy `dispatch-error` status (`isStrandedDispatchError`), clearing its
outcome. Because the rule gives genuine declines the distinct `declined` status, a
`dispatch-error` entry can only be a launch/infra stranding from before [ward#524](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/524), so this
recovers exactly those without ever re-queuing a real per-issue decline.
