---
doc_goal: Let a director operating a read-only surface halt one mis-scoped engineer on demand with `ward agent stop`, understanding that it forwards through the dispatch broker, is stop-only and engineer-only (the same guard reap enforces), and refuses a target that resolves to any other role or to zero / more than one engineer.
---
# ward agent stop: halting one running engineer

`ward agent stop <ref>` halts a single running [engineer](agent-engineer.md)
container on demand. It is the deliberate counterpart to
[`ward agent reap`](agent-reap.md)'s idle sweep: where reap stops engineers idle
past a threshold, `stop` targets one named engineer as a director action.

## The papercut it closes

A [director surface](agent-surface.md) can **dispatch** engineer runs through the
host [dispatch broker](agent-surface.md#dispatching-from-inside-the-surface-session)
but, before [ward#627](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/627),
could not **stop** one. When a director mis-scoped an issue and dispatched an engineer
against it, it had no targeted halt: reap is an idle-killer (1h threshold + CPU guard),
not a deliberate stop, so the director was left commenting a correction and hoping the
run noticed, or asking a human to `docker stop` host-side.

## How it runs

`stop` works **only from a director read-only surface** - the same place ref-mode
dispatch works. The request is forwarded to host ward over the broker's TCP + token
transport, and host ward:

- resolves the target to a container by its `ward.role=engineer` + `ward.repo` +
  `ward.issue` labels (or by a bare container name),
- **fail-closed guards the role**: if the resolved container is anything but
  `ward.role=engineer` (advisor / director / session), it is refused and the role
  named, container untouched,
- refuses a ref that matches **zero** (nothing to stop) or **more than one**
  engineer, listing the candidates rather than guessing,
- `docker stop`s the one match via the same graceful verb reap uses
  ([`agent_reap.go`](../cmd/ward/agent_reap.go)) - no `rm`, no `kill`, no `exec`.

Off a surface (no `WARD_DISPATCH_BROKER_ADDR`) it errors, like a forwarded dispatch
does; halt a run host-side with `docker container stop` instead
([container-stop.md](container-stop.md)).

## What a stop does and does not reap

A stop is `docker container stop`, so it carries the exact reaper semantics
[container-stop.md](container-stop.md) documents: the entrypoint's PID-1 `EXIT`
trap never fires under `SIGTERM`/`SIGKILL`, so the run is **not** salvaged and any
`closes #N` stays as the run last pushed it. Stopping a mis-scoped run is usually
what you want - it does not land half-done work.

## Usage

```bash
ward agent stop coilyco-flight-deck/ward#625   # stop the engineer carrying #625
ward agent stop #625                           # owner/repo inferred from the cwd origin
ward agent stop engineer-claude-ward-625       # stop by container name
ward agent stop #625 --print                   # resolve + show the target, stop nothing
```

## See also

- [docs/agent-reap.md](agent-reap.md) - the idle sweep this is the on-demand counterpart to.
- [docs/agent-surface.md](agent-surface.md) - the director surface stop runs from.
- [docs/container-stop.md](container-stop.md) - the host-side `docker container stop` and its reaper semantics.
- [docs/FEATURES.md](FEATURES.md) - inventory.
