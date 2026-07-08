---
doc_goal: Make an operator grasp the director's surface session as a deliberately least-access, push-blocked containment scope whose whole point is to scope-file-and-dispatch commissioned work into sealed sibling runs - not a passive read-only mode - and understand which layers enforce the no-push edge and where that edge is still soft.
---
# ward agent: the director's read-only surface

The **director's surface session** is the read-only, interactive scope-and-dispatch phase
of [`warded director`](agent-director.md). It is **not a top-level role**: the
old standalone `architect` bring-up now lives only here. Read-only only narrows the
session's push path. It does not remove the director's ability to dispatch sibling
engineers when that is the right follow-through.

A surface session is a seedless interactive bring-up - fresh ephemeral container, fresh clone,
composed operating context, **no issue and no seed** - whose clone **cannot push to its own
remote**. It reads the code, scopes the work, files it, and dispatches it.

## When the director surfaces

The director surfaces a read-only session in two places (see [agent-director.md](agent-director.md)):

- **The init gate.** Before the first drain tick the director asks "drain now?";
  answering **no** surfaces a session first, before any drain.
- **Drain → surface.** When the headless lane drains - nothing queued or in flight -
  the director surfaces a session on the lead repo, then resumes the heartbeat if the queue
  refilled (else stops).

The surface runs on the director's OWN `--driver` and inherits its container/harness flags.
There is no public `warded surface` command.

## What read-only means

Nothing leaves *this clone* - it does **not** seal the session off. Dispatching commissioned
work is the **point**, an **obligation, not a "may"**: every surfaced item is filed
and dispatched (`warded #N` spins a sealed container), not left to die in the
conversation.

**Prefer a sibling dispatch over an in-session subagent.** For delegable work
reach for a sibling warded run (`warded advisor #N`, `warded engineer #N`): it lands a
durable artifact the next run can read, where a subagent dies in scrollback. Reserve a
subagent for read-only fan-out feeding only **your** reasoning.

## What read-only enforces

Layers scope the box to **push-to-this-clone**, not dispatch: the composed `CLAUDE.md`
carries a read-only block; the entrypoint drops `/etc/ward-git-credentials` and
the system `credential.helper` (keeping `FORGEJO_TOKEN` for dispatch); `origin`'s push URL is
stripped to a dead `no-push://` target; a per-clone `pre-push` hook prints a
named wall; and the reaper short-circuits on `WARD_READONLY`, so teardown can't
push. Local `git commit` still works; on exit the [reaper](container-reap.md) sweeps it.

**The soft edge.** The dispatch token is the same bot token, so no-push stays
convention until a **dispatch-only credential** lands.

## Dispatching from inside the surface session

```bash
ward ops forgejo issue create ...    # file the work
warded coilyco-flight-deck/ward#NNN  # dispatch a sealed engineer
```

The surface forwards `warded engineer ...` and `warded advisor ...` ref-mode dispatches to
a host-side broker over TCP (guarded by a per-launch token). Host ward launches the
sibling from the native host context, so Claude/Codex/Goose credentials resolve from the
host home, not the director container. The broker accepts only that constrained
dispatch API; unrelated ward verbs and arbitrary shell never cross it.

Transport is TCP, not a unix-socket bind-mount: under Docker Desktop a bind-mounted host
socket lands as an empty dir, so dispatches dialed a dir.

A surface session is where an operator notices a dispatched run is mis-scoped: stop it
from the surface with [`warded agent stop #N`](agent-stop.md), which forwards a stop
through the same broker (stop-only, engineer-only) - no host-side `docker container stop`
needed. A reserved issue is **immutable** to the run carrying it, so corrections filed
here go to a new issue: see [reserved means immutable](agent-reserved-immutable.md).

## See also

- [docs/agent-director.md](agent-director.md) - the supervisor loop that surfaces this session.
- [docs/agent.md](agent.md) - the `ward agent` roster and the `warded` face.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run.
- [docs/agent-surface-log-read.md](agent-surface-log-read.md) - reading run logs read-only.
