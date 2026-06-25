# ward agent architect

`ward agent architect` (public face `warded architect`) is the **read-only scoping**
role of the startup roster (ward#347, was `explore`): a seedless interactive bring-up -
fresh ephemeral container, fresh clone, composed operating context, **no issue and no
seed** - whose clone **cannot push to its own remote**. You do not run a mode, you send
in your architect: it reads the code, scopes the work, and dispatches it.

The writable seedless `sandbox` was **removed outright** (ward#347): architect is now
the one seedless interactive session, and it is read-only.

"Read-only" means nothing leaves *this clone* - it does **not** seal the session off.
Dispatching commissioned work is the **point**, an **obligation, not a "may"** (ward#320):
every surfaced item is filed as an issue and dispatched (`warded #N` spins its own sealed
container that implements → pushes without touching this clone; ward#315), not left to die
in the conversation. That separates the architect from the supervised
[director](agent-director.md) loop - **capture-and-dispatch, move on without babysitting**.

## Usage

```bash
warded architect                                     # read-only, fresh clone of the cwd's repo
ward agent architect --repo coilyco-flight-deck/ward
ward agent architect --driver codex                  # pick another harness
ward agent architect --repo coilyco-flight-deck/ward --print   # show the docker plan, run nothing
```

It takes no positional argument. The flags: `--repo`, `--with-repo`, `--image`,
`--tag`, `--ward-source`, `--ward-version`, `--aws`, `--print`, `--no-pull`, `--driver`.

## What read-only enforces

Layers scoping the box to **push-to-this-clone**, not dispatch:

1. **Composed restriction (soft).** The entrypoint appends a read-only block to the
   composed `CLAUDE.md`: this clone does not push, but filing + dispatching every
   surfaced item is **obligatory**, unlike the chatty director loop (ward#293, ward#315, ward#320).
2. **Scoped push revoke.** Before the agent drops, the entrypoint removes
   `/etc/ward-git-credentials` and drops the system `credential.helper`, so a push has
   nothing to authenticate with. `FORGEJO_TOKEN` is **kept** for the dispatch-only path.
3. **Stripped push URL (ward#327).** Each clone's `origin` push URL is pointed at a dead
   `no-push://read-only-explore` (`git remote set-url --push`, fetch left intact), so
   the push *target* is gone, not only the credential - on the target and every `--repo` clone.
4. **Pre-push message layer (ward#299).** A per-clone `pre-push` hook prints a clear
   named wall before git reaches the remote. Message layer only - bypassable.
5. **Reaper skips salvage.** The reaper short-circuits on `WARD_READONLY`, so the
   teardown backstop cannot push this tree either.

Local `git commit` still works; on exit the clone is swept by the [reaper](container-reap.md).

**The soft edge (ward#318).** The dispatch token is the *same* bot token, so an agent
could `set-url --push` back and hand-build a push. Stripping the URL (ward#327) raises
the bar; the hard fix is a **dispatch-only credential**, deferred to ward#318.

## Dispatching from inside the architect session

```bash
ward ops forgejo issue create ...    # file the work
warded coilyco-flight-deck/ward#NNN  # dispatch a sealed engineer carry
```

The sibling reads the token from its env, clones fresh, runs its own lifecycle. The
dropped agent is non-root, so the entrypoint group-grants or `socat`-bridges the mounted
docker socket so dispatch works (ward#319).

## `--print`

Renders the docker plan and exits without pulling, cloning, or running. It prints
`access: read-only (...)`, `WARD_READONLY=1`, and the socket bind.

## See also

- [docs/agent.md](agent.md) - the `ward agent` roster and the `warded` face.
- [docs/agent-director.md](agent-director.md) - the supervised loop the architect contrasts with.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run.
