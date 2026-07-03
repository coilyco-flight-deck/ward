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

Both holds are **TTL-bounded** (2h): an older reservation is assumed dead and
reclaimed, so a crashed run never wedges an issue. The local sentinel is also
reclaimed once its container stops running. A detached run leaves its sentinel
for the container's lifetime. `--print` reserves nothing. `--force` skips both
checks to reclaim a stale or foreign hold.

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
