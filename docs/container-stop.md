---
doc_goal: Let an operator halt a runaway detached engineer with confidence - reconstruct the deterministic `engineer-<driver>-<repo>-<issue>` name unaided, use graceful `docker container stop` and not `docker rm -f`, and understand exactly why a stop skips the reaper (a PID-1 EXIT trap never fires) so unpushed work is not auto-salvaged and must be recovered by hand.
---
# Stopping a running container

A detached [engineer](agent-engineer.md) runs unattended, so halting a
mis-scoped or runaway one means stopping its container. An operator who dispatched
the run can always reconstruct the name with nothing to look up, because it is
**deterministic**.

## The deterministic name

`containerRoleName` (the `role == roleEngineer` branch in
[`cmd/ward/container_compute.go`](../cmd/ward/container_compute.go)) builds a container's
name as `engineer-<driver>-<repo>-<issue>`:

- `<driver>` is the `--driver` mode - `claude` (default), `codex`, `goose`, or `opencode`
  (`qwen` is a deprecated alias that still resolves to `opencode`; see [agent-qwen.md](agent-qwen.md)).
- `<repo>` is `safeRepoName`, which **strips the owner** - so [ward#398](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/398) is `...-ward-398`,
  not `...-coilyco-flight-deck-ward-398`.
- `<issue>` is the issue the run is on.

So a claude run on [ward#398](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/398) is **`engineer-claude-ward-398`**.

**Session/surface containers differ.** The issueless roles (a
[director surface](agent-surface.md), an advisor session) take the other
`containerRoleName` branch, `<role>-<driver>-<machine>` - a machine suffix and **no
issue number**. Do not hunt for an issue number on those.

## The stop command

```bash
docker container stop engineer-claude-ward-398
```

Use `docker container stop`, the graceful verb - **not** `docker rm`/`-f`. `rm` is for
**exited** containers (the keep-10 sweep, [container-cleanup.md](container-cleanup.md)),
stop is for a **running** one.

**From a director surface,** you do not reach for the host docker at all: use
[`warded agent stop #N`](agent-stop.md), which forwards a stop through the dispatch
broker to host ward (stop-only, engineer-only). It runs the same graceful `docker
container stop`, so everything below about the reaper applies identically.

## What a stop does and does not reap

The [reaper](container-reap.md) is armed as a bash `trap ... EXIT` on the entrypoint,
which runs as the container's **PID 1** (no `--init` shim, and the only trap is `EXIT` -
there is no `SIGTERM`/`SIGINT` handler). `docker container stop` sends `SIGTERM`, then
`SIGKILL` after the grace period. A PID-1 process with no handler for a signal has that
signal **ignored by the kernel**, so the `SIGTERM` is dropped and the follow-up `SIGKILL`
is uncatchable. Either way **the `EXIT` trap never fires, so the reaper does not run.**

An externally stopped run is therefore **not** salvaged: nothing is committed to a
`ward-salvage/<id>` branch, no salvage issue is filed, and any `closes #N` stays as the
run last pushed it (the issue is **not** reopened). That is usually what you want when
killing a mis-scoped run - you do not want it landing half-done work - but know that
in-progress, unpushed work in the stopped run is not auto-preserved. The container is
left `exited` (the container is `-d`, not `--rm`), so its writable layer survives until the
keep-10 sweep, and work can be recovered by hand (`docker cp`, `docker logs`) in that
window.

## See also

- [docs/agent-engineer.md](agent-engineer.md) - the detached engineer this stops.
- [docs/agent-stop.md](agent-stop.md) - `warded agent stop`, the same stop reached from a director surface through the broker.
- [docs/container-reap.md](container-reap.md) - the teardown reaper a stop skips.
- [docs/container-cleanup.md](container-cleanup.md) - the `docker rm` sweep of exited containers.
- [docs/FEATURES.md](FEATURES.md) - inventory.
