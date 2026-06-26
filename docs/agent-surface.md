# ward agent: the director's read-only surface

The **director's surface session** is the read-only, interactive scope-and-dispatch phase
of [`warded director`](agent-director.md) (ward#353). It is **not a top-level role**: the
old standalone `architect` bring-up now lives only here.

A surface session is a seedless interactive bring-up - fresh ephemeral container, fresh clone,
composed operating context, **no issue and no seed** - whose clone **cannot push to its own
remote**. It reads the code, scopes the work, files it, and dispatches it.

## When the director surfaces

The director surfaces a read-only session in two places (see [agent-director.md](agent-director.md)):

- **The init gate (ward#361).** Before the first drain tick the director asks "drain now?";
  answering **no** surfaces a session first, before any drain.
- **Drain → surface (ward#351).** When the headless lane drains - nothing queued or in flight -
  the director surfaces a session on the lead repo, then resumes the heartbeat if the queue
  refilled (else stops).

The surface runs on the director's OWN `--driver` and inherits its container/harness flags
(ward#355). There is no public `warded surface` command.

## What read-only means

Nothing leaves *this clone* - it does **not** seal the session off. Dispatching commissioned
work is the **point**, an **obligation, not a "may"** (ward#320): every surfaced item is filed
and dispatched (`warded #N` spins its own sealed container), not left to die in the
conversation. The director heartbeat polls outcomes after you exit.

**Prefer a sibling dispatch over an in-session subagent (ward#374).** For delegable work - a
design, a research dig, an implementation - reach for a sibling warded run (`warded advisor
#N`, `warded engineer #N`) before a subagent: the sibling lands a durable, attributable
artifact (issue thread, pushed commit) the next carry can read, where a subagent's output
dies in scrollback. Reserve a subagent for read-only fan-out feeding only **your** reasoning.

## What read-only enforces

Layers scope the box to **push-to-this-clone**, not dispatch: the composed `CLAUDE.md`
carries a read-only block (ward#293); the entrypoint drops `/etc/ward-git-credentials` and
the system `credential.helper` (keeping `FORGEJO_TOKEN` for dispatch); `origin`'s push URL is
stripped to a dead `no-push://` target (ward#327, fetch intact), so the push *target* is gone,
not just the credential; a per-clone `pre-push` hook prints a named wall (ward#299,
bypassable); and the reaper short-circuits on `WARD_READONLY`, so teardown can't push either.
Local `git commit` still works; on exit the clone is swept by the [reaper](container-reap.md).

**The soft edge (ward#318).** The dispatch token is the same bot token, so the no-push rule
is convention until a **dispatch-only credential** lands (ward#318).

## Dispatching from inside the surface session

```bash
ward ops forgejo issue create ...    # file the work
warded coilyco-flight-deck/ward#NNN  # dispatch a sealed engineer carry
```

The surface forwards `warded engineer ...` and `warded advisor ...` ref-mode dispatches to
a host-side broker mounted into the session. Host ward then launches the sibling from the
native host context, so Claude/Codex/Goose credentials resolve from the host home rather
than from the director container. The broker accepts only that constrained dispatch API;
unrelated ward verbs and arbitrary shell never cross it. The sibling still clones fresh and
runs its own lifecycle.

## See also

- [docs/agent-director.md](agent-director.md) - the supervisor loop that surfaces this session.
- [docs/agent.md](agent.md) - the `ward agent` roster and the `warded` face.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run.
