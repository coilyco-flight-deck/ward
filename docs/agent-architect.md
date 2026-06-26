# ward agent architect

`ward agent architect` (public face `warded architect`) is the **read-only scoping**
role of the startup roster (ward#347, was `explore`): a seedless interactive bring-up -
fresh container, fresh clone, composed context, **no issue and no seed** - whose clone
**cannot push to its own remote**. It reads the code, scopes the work, and dispatches it.
The writable seedless `sandbox` was **removed outright** (ward#347); architect is the one
seedless interactive session, and it is read-only.

"Read-only" means nothing leaves **this clone**, not that the session is sealed off.
Dispatching commissioned work is the **point**, an **obligation, not a "may"** (ward#320):
every surfaced item is filed and dispatched (`warded #N` spins its own sealed container
that implements → pushes without touching this clone; ward#315), not left to die in the
conversation - **capture-and-dispatch, move on without babysitting**, unlike the
supervised [director](agent-director.md) loop.

**Prefer a sibling dispatch over an in-session subagent (ward#374).** For delegable work -
a design, a research dig, an implementation - reach for a sibling warded run (`warded
advisor #N`, `warded engineer #N`) before an in-session subagent. The sibling lands a
durable, attributable artifact (issue thread, pushed commit) the next carry can read,
where a subagent's output dies in scrollback and in a read-only clone buys no extra reach.
Reserve a subagent for read-only fan-out feeding only **your** reasoning.

## Usage

```bash
warded architect   # read-only, fresh clone of the cwd's repo
ward agent architect --repo coilyco-flight-deck/ward --driver codex
ward agent architect --repo coilyco-flight-deck/ward --print   # docker plan, run nothing
```

It takes no positional argument. `--help` lists the flags (`--repo`, `--with-repo`,
`--driver`, `--print`, `--no-pull`, and the image/source pins).

## What read-only enforces

Layers scoping the box to **push-to-this-clone**, not dispatch:

1. **Composed restriction (soft).** The entrypoint appends a read-only block to the
   composed `CLAUDE.md`: no push, but filing + dispatching every surfaced item is
   **obligatory** (ward#293, ward#315, ward#320).
2. **Scoped push revoke.** The entrypoint removes `/etc/ward-git-credentials` and drops
   `credential.helper`, so a push cannot authenticate. `FORGEJO_TOKEN` is **kept** for dispatch.
3. **Stripped push URL (ward#327).** Each clone's `origin` push URL points at a dead
   `no-push://read-only-explore` (`set-url --push`, fetch intact), so the push **target**
   is gone, not only the credential.
4. **Pre-push message layer (ward#299).** A per-clone `pre-push` hook prints a clear
   named wall before git reaches the remote. Message layer only - bypassable.
5. **Reaper skips salvage.** The reaper short-circuits on `WARD_READONLY`, so the
   teardown backstop cannot push this tree either.

Local `git commit` still works; on exit the clone is swept by the [reaper](container-reap.md).

**The soft edge (ward#318).** The dispatch token is the **same** bot token, so an agent
could `set-url --push` back and hand-build a push. A **dispatch-only credential** is the
proper fix, deferred to ward#318.

## Dispatching from inside the architect session

```bash
ward ops forgejo issue create ...    # file the work
warded coilyco-flight-deck/ward#NNN  # dispatch a sealed engineer carry
```

The sibling reads the token from its env and runs its own lifecycle. The non-root agent
reaches the docker socket via a group-grant or `socat` bridge (ward#319).

## `--print`

Renders the docker plan and exits without pulling, cloning, or running, printing
`access: read-only (...)`, `WARD_READONLY=1`, and the socket bind.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster and the `warded` face.
- [docs/agent-director.md](agent-director.md) - the supervised loop the architect contrasts with.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run.
