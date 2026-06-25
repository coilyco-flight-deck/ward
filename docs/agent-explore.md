# ward agent explore

`ward agent explore` is the **read-only** sibling of
[`ward agent sandbox`](agent-sandbox.md): the same seedless interactive bring-up -
a fresh ephemeral container with a fresh clone plus the composed operating context,
**no issue and no seed** - except this clone **cannot push to its own remote**.

"Read-only" means exactly that and no more: nothing leaves *this clone*. It does
**not** seal the session off - dispatching commissioned work is the **point** of
explore, and it is an **obligation, not a "may"** (ward#320). Every work item the
session surfaces - a bug, a missing test, a follow-up - must be filed as an issue and
dispatched headless (`warded #N` spins its own sealed container that implements,
commits, merges, and pushes without touching this clone; ward#315), not left to die in
the conversation. That discipline separates explore from the **supervised backlog
loop**, which polls outcomes, surfaces blockers, and does chatty back-and-forth with a
human in the seat. Explore is the opposite: **capture-and-dispatch and move on without
babysitting**.

## Usage

```bash
warded explore                                     # claude, read-only, fresh clone of the cwd's repo
ward agent explore --repo coilyco-flight-deck/ward
ward agent explore --driver codex                  # pick another harness
ward agent explore --repo coilyco-flight-deck/ward --print   # show the docker plan, run nothing
```

It takes no positional argument. The flags mirror [`sandbox`](agent-sandbox.md):
`--repo`, `--with-repo`, `--image`, `--tag`, `--ward-source`, `--aws`, `--print`,
`--no-pull`, `--driver`.

## What read-only enforces

Layers that scope the box to **push-to-this-clone**, not to dispatch:

1. **Composed restriction (soft).** The entrypoint appends a read-only block to the
   composed `CLAUDE.md`: this clone does not push, but filing and dispatching every
   surfaced work item is **obligatory**, in contrast to the chatty backlog loop
   (ward#293, ward#315, ward#320).
2. **Scoped push revoke.** Before the agent drops, the entrypoint removes
   `/etc/ward-git-credentials` and drops the system `credential.helper`, so a push
   from this clone has nothing to authenticate with. `FORGEJO_TOKEN` is **kept** for
   the dispatch-only path.
3. **Pre-push message layer (ward#299).** A per-clone `pre-push` hook prints a clear
   named wall before git reaches the remote (the bare revoke fails with an opaque
   `could not read Username`). Message layer only - bypassable (`--no-verify`, `rm`).
4. **Reaper skips salvage.** The reaper short-circuits on `WARD_READONLY`, so the
   teardown backstop cannot push this clone's tree either.

Local `git commit` still works (harmless); on exit the clone is swept by the [reaper](container-reap.md).

**The soft edge (ward#318).** The dispatch token is the *same* bot token, so a
determined agent could hand-build a push URL. The restriction forbids it, but it is a
convention - the hard fix is a **dispatch-only credential**, deferred to ward#318.

## Dispatching from inside explore

```bash
ward ops forgejo issue create ...    # file the work
warded coilyco-flight-deck/ward#NNN  # dispatch a sealed headless fix
```

The sibling resolves the token from the container env, clones fresh, runs its own lifecycle.

**Socket access.** The dropped agent is non-root. For a group-owned socket (the common
`root:docker 0660`), `grant_docker_socket_access` joins the socket's owning group; for a
`root:root` one, a root `socat` bridge exposes an agent-group-owned socket via
`DOCKER_HOST` (ward#319).

## `--print`

Renders the docker plan and exits without pulling, cloning, or running. It prints
`access: read-only (...)`, `WARD_READONLY=1`, and the docker socket bind.

## See also

- [docs/agent-sandbox.md](agent-sandbox.md) - `sandbox`, the writable sibling.
- [docs/agent.md](agent.md) - the `ward agent` umbrella.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run.
