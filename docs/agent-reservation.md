---
doc_goal: Show how a run claims an issue so no two runs duplicate work across hosts - the local sentinel plus remote Forgejo marker, TTL reclaim, release-on-pre-launch-death, and the stale-host-binary nudge - so an operator can reason about why a run refused, retried, or freed a hold.
---
# ward agent: reservation and host checks

How a `ward agent` run avoids double-work and nudges a stale host binary. See
[docs/agent.md](agent.md).

## Reservation (no double-work)

Before a container fires, the run **reserves the issue** so a second run never
works it at once, on this host or another:

- **Local file sentinel.** `~/.ward/agent-reservations/<owner>-<repo>-issue-<N>.json`
  records the container holding the issue. A fresh sentinel whose container is
  still running blocks a new run on the same host.
- **Remote Forgejo comment.** The run posts a marker comment (`🔒 Reserved by
  ward agent ...`) on the issue and refuses to start if it finds a fresh one
  already there - that's another host carrying the issue. When the dispatch cleared
  an explicit **GO** [pre-flight](agent-preflight.md), that comment folds in the
  collapsed GO read, so the reservation records *why* the issue was judged
  carriable.

## Self-documenting on the tracker ([ward#609](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/609))

The reservation comment is posted **before** the in-container auth smoke gate
([ward#222](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/222)), so a run that dies at that gate still left the issue thread a record of
**what** it was carrying. That makes the tracker, not docker logs, the primary
diagnostic surface. Three surfaces, each its own job:

- **Reservation comment = WHAT.** The comment folds the **dynamic** per-run seed
  context into a collapsed `<details>` block: the resolved ref, target branch,
  driver, run id, dispatch timestamp, the landing workflow, and which thread
  comments were **included vs stripped** in the pre-flight read (ward strips its
  own automation - reservation pings and NO-GO verdicts). The static container
  doctrine and seed boilerplate are identical every run, so they are
  **referenced by ward version, never pasted**. No secret-bearing content is
  added: comment **bodies** are never included (only author + timestamp), and
  the issue body stays on the issue page instead of being re-pasted into the
  reservation comment.
- **Reservation-released comment = WHY + RECOVER.** When a container dies at a
  pre-launch gate, the reaper's release comment names the **specific gate** that
  failed (`auth`, `ollama-probe`, or `bootstrap`), folds in the actual error line,
  and gives the recovery step (for `auth`: refresh the host claude login, then
  re-dispatch). See [container-reap.md](container-reap.md).
- **Docker-log echo = BACKSTOP.** The container entrypoint echoes the same dynamic
  context (plus the seed/task text) to stdout at startup, **before any gate**, as a
  delimited greppable `ward run context` banner - the last-resort surface for an
  abort that never reaches a tracker comment.

Both holds are **TTL-bounded** (1h by the smart-defaults bundle): an older
reservation is assumed dead and reclaimed, so a crashed run never wedges an
issue. The local sentinel is also reclaimed once its container stops running. A
detached run leaves its sentinel for the container's lifetime. `--print`
reserves nothing. `--force` skips both checks to reclaim a stale or foreign
hold.

## Two runs on the same tick: jittered re-check + broker lock ([ward#600](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/600))

The reservation **check** and **post** are not one atomic step, so two dispatches
firing on the same tick can each read "not reserved", each post, and each spin a
container - `ward#507` collected **four** reservation comments in 17 seconds. Two
guards close that race:

- **Broker per-ref lock (primary).** A dispatch routed through the host dispatch
  broker (a director surface asking host ward to launch a sibling) takes a
  **per-issue-ref mutex** before it runs, so two `warded #N` for the same `N`
  **serialize at the broker before any container starts**: the second waits, then
  sees the first's reservation and yields with zero wasted spin. A distinct ref
  never contends, so unrelated dispatches stay parallel.
- **Jittered double-check (backstop).** For any path the broker lock does not
  cover - two direct host runs, or two hosts - each run, after posting its
  reservation, waits a **randomized** interval (default `[5s,15s]`, the jitter
  breaks the same-tick symmetry so the waiters don't just re-collide) and
  **re-reads** the thread. If a concurrent reservation now wins a deterministic
  tiebreak - **earliest timestamp, else the lexically-min `container@host`
  identity** - this run yields and tears down before any work, leaving its own
  reservation comment to lapse at the TTL (a release marker would wrongly free the
  winner). Exactly one run wins, because every racer computes the same total order
  over the same thread. The wait is set by `WARD_AGENT_RESERVE_RECHECK` (a
  duration ceiling, min derives as a third of it; `0`/`off` disables it). A
  detached run has no human watching, so the wait costs no visible latency, and
  `--force` skips the re-check like the pre-post check.

## Skip an already-closed issue ([ward#600](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/600))

Before any container spins, `ward agent` reads the issue state. A **closed** issue
is already landed, so the run **no-ops and tears down** with the `issue-closed`
dispatch exit code ([agent-dispatch-contract.md](agent-dispatch-contract.md))
rather than spinning a container to rediscover "already done" - the `#507`
restart-loop that burned ~3 spins. `--force` works a closed issue anyway (a
deliberate re-open-and-carry), and `--print` still renders its dry run.

The remote comment is the **only** cross-host dedup + thread signal, so a failed
post is not silent: it retries, then warns with
the greppable token `remote reservation NOT posted`. On the **broker-dispatched**
path stderr goes to `~/.ward/agent-logs/dispatch/*.log`, so that token lets an
operator `grep` those logs, checking the host Forgejo token/SSM path first.

## Reserved means immutable

While a reservation stands, the issue is **immutable** to the run carrying it - it
seeds the body once and never re-reads. A correction found after dispatch goes to
a **new issue**, not an edit or comment on the reserved one, which ward best-effort
**locks** where the API allows: see
[reserved means immutable](agent-reserved-immutable.md).

## Pre-launch death releases the hold

A container that dies at the [ward#222 smoke test](agent.md) did nothing, yet its
hold blocks a plain retry for the full TTL. So on a clean teardown where the
agent never launched, the [reaper](container-reap.md) posts a **release marker
comment**, and `freshReservationComment` frees a reservation once a release is
posted at or after it (newest marker of each kind wins), so the retry needs no `--force`.
That release comment now names the **gate** that failed and the recovery step
([ward#609](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/609), above), so it doubles as the diagnostic an operator otherwise went to
docker logs for. The entrypoint records the failing gate to a small in-container
file (`WARD_GATE_FAILURE_FILE`, default `/run/ward/gate-failure`) that the reaper
reads; an unclassified pre-launch death still gets the generic release comment.

### The release is loud, not benign ([ward#595](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/595))

A bare reservation-release read as benign housekeeping ("hold retracted, retry needs
no `--force`") even though it means **the run never started and no work happened**. When
no incidental second attempt landed, the issue was silently **orphaned**: open,
reservation released, nothing running, nothing signalling failure. So the release comment
now leads with a loud **"⚠️ Run never started — this issue needs re-dispatch"** headline
and carries a distinct machine-detectable marker (`<!-- ward-needs-redispatch -->`,
`agentNeedsRedispatchMarker`) an operator can grep for. Two consumers close the gap:

- **A `ward agent director` re-queues it automatically.** When the reconcile pass sees a
  dispatched entry whose container is gone and the thread carries a release marker stamped
  **at/after** its dispatch (a pre-launch death, not a run that launched and vanished), it
  re-queues the entry rather than parking it terminal `failed` - deterministic re-dispatch,
  the incidental [ward#593](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/593) failover made a guarantee. This is bounded by `redispatchAttemptCap`
  (3): a persistently-sick host exhausts the cap and parks the entry `blocked` with the
  `orphaned-needs-redispatch` outcome (a human must fix the host or re-dispatch by hand),
  never a livelock. See [container-reap.md](container-reap.md) and
  [agent-director-dispatch.md](agent-director-dispatch.md).
- **A bare surface-session dispatch** (not tracked by any director ledger, the [ward#594](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/594) case)
  leaves the loud marker on the thread as the machine-detectable "needs re-dispatch" signal
  an operator or heartbeat greps, rather than a quiet release that reads as done.

## Launch failure rolls the reservation back

Tracked in [ward#570](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/570).
The reservation is taken **before** `docker run`. If the launch itself fails
after that - a docker disk sweep, the env-file write, or `docker run` exiting 125
(the [snap-docker env-file bug, ward#569](agent.md)) - then no container ever
exists, so both halves of the hold are lies. The dispatch path arms a rollback
the moment it acquires the reservation and disarms it only once the container is
confirmed up; a failure in that window retracts **both** halves - it deletes the
local sentinel and posts the same **release marker comment** the reaper uses,
plus an unlock. A human reading the issue then sees the road-block retracted, and
a re-run needs no `--force`. The rollback is best-effort and loud: a failed
release post warns but never masks the original launch error.

Retracting the road-block by **appending** a release marker keeps the launched
rollback consistent with the reaper, but it leaves the original
`ward-agent-reservation` comment on the thread as visible-if-superseded noise.
An operator who wants that stray comment **gone**, not merely retracted, can now
delete it directly: `ward ops forgejo issue-comment delete <owner> <repo> <id>`
resolves `issueDeleteComment` (DELETE /repos/{owner}/{repo}/issues/comments/{id},
`--dry-run` prints the resolved request first). Before this leaf the surface
exposed only `list`, so removing an orphaned reservation comment meant the web UI
by hand ([ward#570](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/570)).
The delete is a targeted-ID call, `coily*`-owner-scoped like
every other `{owner}` leaf, and irreversible - it is a cleanup verb, not part of
the automatic rollback, which stays on the release-marker path.

For an interactive dispatch the cheap reservation check runs **before the LLM
pre-flight**, not after: an issue another run already holds
short-circuits up front rather than wasting a full model read. The precheck reuses
the already-fetched thread and never takes the hold. `--force` bypasses both.

## Host stale-ward reminder

A `ward agent` run installs ward *inside* the container and logs its `ward
version` there. When the run is detached, no human watches that log, so the cue
that the **host** ward binary is itself behind a release is lost. To keep that
awareness, ward does a best-effort check at the host dispatch moment: it resolves
the latest `coilyco-flight-deck/ward` release tag and, if the host binary is
behind it, prints a two-line stderr reminder pointing at
[`ward upgrade`](../README.md).

The lookup routes through the in-binary [`ward ops forgejo`](ops-forgejo-in-ward.md)
`release list` specverb, whose `--query "[0].tag_name"` projection returns
the newest published non-prerelease tag via the audited SSM-authed leaf.

The check is quiet and non-blocking: a `dev`/source build, no network, an auth
wall, or an unparseable tag all stay silent, and a 5s timeout keeps a slow
Forgejo from holding up the dispatch. It is skipped under `--print`
and compares only the release tag (the container pins its own).

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/agent-preflight.md](agent-preflight.md) - the LLM pre-flight the precheck runs ahead of.
