# ward agent explore

`ward agent explore` is the **read-only** sibling of
[`ward agent sandbox`](agent-sandbox.md): the same seedless interactive bring-up -
a fresh ephemeral container with a fresh clone plus the composed operating context,
**no issue and no seed** - except this clone **cannot push to its own remote**.

"Read-only" means exactly that and no more: nothing leaves *this clone*. It does
**not** seal the session off. Dispatching commissioned work is the **point** of
explore - you read and reason in a no-direct-push box, and the natural product is
**file the issue, dispatch the headless fix**. So explore keeps a **dispatch-only
capability**: the forgejo token survives the revoke (it can file issues and launch a
sibling run) and the host docker socket is mounted (so `warded #N` works from inside).
The dispatched run does its own implement -> commit -> merge -> push in its **own**
sealed container, never touching this clone (ward#315).

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
   composed `CLAUDE.md`: this clone does not push, but filing issues and dispatching
   `warded #N` are encouraged (ward#293, ward#315).
2. **Scoped push revoke.** Before the agent drops, the entrypoint removes
   `/etc/ward-git-credentials` and drops the system `credential.helper`, so a push
   from this clone has nothing to authenticate with. `FORGEJO_TOKEN` is **kept** for
   the dispatch-only path.
3. **Pre-push message layer (ward#299).** A per-clone `pre-push` hook prints a clear
   named wall before git reaches the remote (the bare revoke fails with an opaque
   `could not read Username`). Message layer only - bypassable (`--no-verify`, `rm`).
4. **Reaper skips salvage.** The reaper short-circuits on `WARD_READONLY`, so the
   teardown backstop cannot push this clone's tree either.

Local `git commit` still works (harmless). On exit the clone is swept by the
[reaper](container-reap.md).

**The soft edge (ward#315).** The dispatch token is the *same* bot token, so a
determined agent could hand-build a push URL. The restriction forbids it, but it is a
convention - the hard fix is a **dispatch-only credential**, deferred to ward#315.

## Dispatching from inside explore

```bash
ward ops forgejo issue create ...    # file the work
warded coilyco-flight-deck/ward#NNN  # dispatch a sealed headless fix
```

The sibling resolves the token from the container's env (no host SSM/AWS), clones
fresh, and runs its own lifecycle.

> **Open item (ward#315):** the dropped agent is non-root but the mounted socket is
> root-owned, so the socket-access grant has a host side-effect (chmod vs. a socat
> bridge). Not wired yet - dispatch fails on a socket permission error until it lands.

## `--print`

Renders the docker plan and exits without pulling, cloning, or running. It prints
`access: read-only (...)`, `WARD_READONLY=1`, and the docker socket bind. Safe with
no docker daemon.

## See also

- [docs/agent-sandbox.md](agent-sandbox.md) - `sandbox`, the writable sibling.
- [docs/agent.md](agent.md) - the `ward agent` umbrella.
- [docs/container-reap.md](container-reap.md) - the reaper that sweeps the run.
