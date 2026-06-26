# ward agent: the director's read-only surface

The **director's surface session** is the read-only, interactive scope-and-dispatch phase
of [`warded director`](agent-director.md) (ward#353). It is **not a top-level role**: ward#353
collapsed the old standalone `architect` role into the director, reducing the roster to three
(engineer / director / advisor). The architect's read-only bring-up now lives only here.

A surface session is a seedless interactive bring-up - fresh ephemeral container, fresh clone,
composed operating context, **no issue and no seed** - whose clone **cannot push to its own
remote**. It reads the code, scopes the work, files it, and dispatches it.

## When the director surfaces

The director surfaces a read-only session in two places (see [agent-director.md](agent-director.md)):

- **The init gate (ward#361).** Before the first drain tick the director asks "drain now?";
  answering **no** surfaces a session first, so you can scope and dispatch *before* any
  autonomous drain. This is the "just scope now" path the old `warded architect` served.
- **Drain → surface (ward#351).** When the headless lane drains - nothing queued or in flight -
  the director surfaces a session on the lead repo for new direction, then resumes its
  heartbeat if the queue refilled (else stops).

The surface runs on the director's OWN `--driver` and inherits its container/harness flags
(ward#355). It is internal plumbing: there is no `warded surface` command, and `warded
architect` now errors as an unknown command.

## What read-only means

Nothing leaves *this clone* - it does **not** seal the session off. Dispatching commissioned
work is the **point**, an **obligation, not a "may"** (ward#320): every surfaced item is filed
and dispatched (`warded #N` spins its own sealed container that implements → pushes without
touching this clone; ward#315), not left to die in the conversation. The job is
**capture-and-dispatch and move on without babysitting** - the director heartbeat that
surfaced you polls outcomes and reconciles the lane once you exit back to it.

## What read-only enforces

Layers scoping the box to **push-to-this-clone**, not dispatch:

1. **Composed restriction (soft).** A read-only block in the composed `CLAUDE.md`: this clone
   does not push, but filing + dispatching is **obligatory** (ward#293).
2. **Scoped push revoke.** The entrypoint removes `/etc/ward-git-credentials` and drops the
   system `credential.helper`. `FORGEJO_TOKEN` is **kept** for the dispatch-only path.
3. **Stripped push URL (ward#327).** The `origin` push URL is pointed at a dead `no-push://`
   target (fetch left intact), so the push *target* is gone, not just the credential.
4. **Pre-push message layer (ward#299).** A per-clone `pre-push` hook prints a clear named
   wall before git reaches the remote. Bypassable message layer only.
5. **Reaper skips salvage.** The reaper short-circuits on `WARD_READONLY`, so the teardown
   backstop cannot push this tree either.

Local `git commit` still works; on exit the clone is swept by the [reaper](container-reap.md).

**The soft edge (ward#318).** The dispatch token is the *same* bot token, so an agent could
`set-url --push` back. Stripping the URL raises the bar; the hard fix is a **dispatch-only
credential**, deferred to ward#318.

## Dispatching from inside the surface session

```bash
ward ops forgejo issue create ...    # file the work
warded coilyco-flight-deck/ward#NNN  # dispatch a sealed engineer carry
```

The sibling reads the token from its env, clones fresh, runs its own lifecycle. The dropped
agent is non-root, so the entrypoint group-grants or `socat`-bridges the mounted docker
socket so dispatch works (ward#319).

## See also

- [docs/agent-director.md](agent-director.md) - the supervisor loop that surfaces this session.
- [docs/agent.md](agent.md) - the `ward agent` roster and the `warded` face.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run.
