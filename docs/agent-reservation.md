# ward agent: reservation and host checks

How a `ward agent` run avoids double-work and nudges a stale host binary. See
[docs/agent.md](agent.md) for the verb family.

## Reservation (no double-work)

Before a container fires, the run **reserves the issue** so a second run never
works it at the same time - on this host or another:

- **Local file sentinel.** `~/.ward/agent-reservations/<owner>-<repo>-issue-<N>.json`
  records the container holding the issue. A fresh sentinel whose container is
  still running blocks a new run on the same host.
- **Remote Forgejo comment.** The run posts a marker comment (`🔒 Reserved by
  ward agent ...`) on the issue and refuses to start if it finds a fresh one
  already there - that's another host carrying the issue.

Both holds are **TTL-bounded** (2h): an older reservation is assumed dead and
reclaimed, so a crashed run never wedges an issue. The local sentinel is also
reclaimed the moment its container is no longer running. An attached `work` run
releases its sentinel when it returns; a detached run (`headless`, `task`,
`--detach`) leaves it for the container's lifetime. Remote/network failures
degrade to a warning - the local sentinel still guards this host. `--print`
reserves nothing (a dry run). `--force` skips both checks to reclaim a stale or
foreign hold.

## Pre-launch death releases the hold (ward#264)

A container that dies at the [ward#222 smoke test](agent.md) did nothing, yet its
remote hold blocks a plain retry for the full TTL. So on a clean teardown where the
agent never launched, the [reaper](container-reap.md) posts a **release marker
comment**, and `freshReservationComment` treats a reservation as free once a
release was posted at or after it (newest marker of each kind wins). The retry
then needs no `--force`.

For an interactive `headless` dispatch the cheap reservation check runs **before
the LLM pre-flight**, not after (ward#184): an issue another run already holds
short-circuits up front rather than wasting a full model read only to fail at the
launch-time gate. The precheck reuses the thread already fetched for the pre-flight
(no extra Forgejo call) and never takes the hold itself - the authoritative
two-sided reservation still happens at launch. `--force` bypasses both.

## Host stale-ward reminder (ward#143)

A `ward agent` run installs ward *inside* the container and logs its `ward
version` there. When the run is non-interactive - `headless`/`task` (always
detached) or `work --detach` - no human watches that log, so the cue that the
**host** ward binary is itself behind a release is lost. To keep that awareness,
ward does a best-effort check at the host dispatch moment (where the operator
still is): it resolves the latest `coilyco-flight-deck/ward` release tag and, if
the host binary is behind it, prints a two-line stderr reminder pointing at
[`ward upgrade`](../README.md).

The lookup routes through the in-binary [`ward ops forgejo`](ops-forgejo-in-ward.md)
specverb (ward#172) rather than a hand-rolled HTTP client: it shells the host
ward to `ops forgejo release list <owner> <repo> --draft=false --pre-release=false
--query "[0].tag_name" --output text`, so the audited, SSM-authed guardfile leaf
does the call and the `--query` projection hands back just the scalar tag. The
filters mirror the old `/releases/latest` semantics (newest published, non-draft,
non-prerelease).

The check is deliberately quiet and non-blocking: a `dev`/source build, no
network, an auth wall, or an unparseable tag all stay silent rather than guess,
and a 5s timeout means a slow Forgejo never holds up the dispatch. It is skipped
under `--print` (a pure offline dry run). It compares only the release tag, not
the in-container ward, because the container always pins/downloads its own.

## See also

- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
- [docs/agent-preflight.md](agent-preflight.md) - the LLM pre-flight the precheck runs ahead of.
