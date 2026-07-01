# ward container cleanup

A ward [container](container.md) is throwaway by design, but a stopped container
is not gone: it keeps its writable layer (~380 MB each) until something runs
`docker rm`. Left alone they pile up. Issue
[ward#272](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/272)
caught 110 exited `ward-*` containers holding 41.76 GB - 99% of the docker disk -
which broke the **whole fleet** silently:

- New `docker run` fails at layer creation: `no space left on device`.
- Containers that do start hang: the agent can't write `~/.claude/` to a full
  disk, so it wedges instead of erroring (misread as an auth failure, ward#222).

## Why not the reaper

The obvious home is the [reaper](container-reap.md), but it can't remove its own
container two ways over: it runs **inside** the container as a `trap ... EXIT`
(so the container is still up when it fires), and the image mounts no docker
socket. `docker run --rm` would self-clean but takes the container log with it on
exit, killing the `docker logs` post-mortem the reaper's last-resort patch dump
relies on.

## The sweep

Reclamation is host-side instead. Every container-launching dispatch (`ward
agent engineer`, `ward agent advisor` freeform) first sweeps exited
ward containers before adding one more:

1. List exited containers carrying the `ward=true` label, newest first
   (`docker ps -a --filter label=... --filter status=exited`).
2. Keep the most recent `containerReapKeep` (10) for `docker logs` post-mortem.
3. **Drain** the older tail to `~/.ward/agent-logs/<container>/` (console log,
   transcript, `meta.json`) **before** removing it - the `rm` takes the log and
   the writable layer with it, so the drain is ordered first (ward#363,
   [agent-observability.md](agent-observability.md)).
4. `docker rm` the older tail (no `-f`: only already-exited containers are ever
   targeted, so a running run is never touched).

Stopping a carry is a different lifecycle. This sweep `docker rm`s an **exited**
container to reclaim disk. To halt a still-**running** carry (a mis-scoped one killed
mid-flight), the verb is `docker container stop <name>`, not `rm`/`-f` - see
[container-stop.md](container-stop.md) for the deterministic name and what a stop reaps.

An engineer carry additionally clears any **exited same-name** container before it
launches: its name is deterministic (`engineer-<driver>-<repo>-<N>`, ward#364), so a
prior attempt on the same issue still inside the keep-10 window would otherwise
collide on the docker name. Only an exited corpse is force-removed (a live duplicate
is already blocked by the reservation); the issueless roles carry a machine suffix
and never collide.

This is **self-correcting**: the same fleet activity that creates dead containers
is what prunes them, exactly when disk pressure would otherwise build. It is
best-effort - no docker, a down daemon, or a raced `rm` is logged and stepped
past, never a launch failure. Because the reaper preserves residual work to the
**remote** before exit, a swept container loses only its log, never work.

One interaction to know: the catastrophic dead-PAT recovery path in
[container-reap.md](container-reap.md) reads its patch dump off a *surviving*
container's `docker logs`. That window is now the keep-10 most-recent exited
containers - where a just-failed run lives anyway - so recover promptly; older
containers past the window are gone.

## See also

[docs/container.md](container.md) - container subsystem.
[docs/container-reap.md](container-reap.md) - the teardown reaper.
[docs/container-stop.md](container-stop.md) - stopping a running carry (`docker container stop`, not `rm`).
[docs/FEATURES.md](FEATURES.md) - inventory.
