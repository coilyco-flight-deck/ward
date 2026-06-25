# Root credential broker (ward side)

The **root credential broker** hardens read-only [`explore`](agent-explore.md):
today the session keeps `FORGEJO_TOKEN` in the agent's env, the *same* bot token a
determined agent could rebuild a push from (the soft edge, ward#318). The broker
closes that gap - a **root daemon** holds the token; the dropped, non-root agent
reaches the forge through a unix socket, holding nothing.

This is the **ward side** (ward#329 Unit B). The **policy core** - protocol,
authorizer, executor interface, server - lives in `cli-guard/pkg/broker`
(cli-guard#167). The split: *policy core in cli-guard, glue + credential in ward.*

## The pieces

- **`ward container broker`** (`cmd/ward/broker.go`) is the daemon's `main`: it
  resolves the root-held `FORGEJO_TOKEN`, opens the socket and permissions it
  `root:<agent-gid>` mode `0660` (group-readable, never world), then serves until
  a signal cancels it. Socket creation is the caller's job; `broker.Server` only
  listens on an already-permissioned socket.
- **The executor** (`cmd/ward/broker_exec.go`) shells
  `ward-kdl-write ops forgejo <verb>` for **file / edit / comment issue**, seeding
  the bot token into the subprocess env (env, never argv).
- **The authorizer** is the write tier: the file/edit/comment op allowlist +
  `broker.Policy`'s invariants + a `coily*` owner gate. `dispatch` is **not**
  allowed, so it is refused out-of-tier before the executor runs (routing is Unit C).

## ward-kdl-write + auth

The executor shells the **write tier** (ward#240): `read + create/edit`, delete
absent at compile time. The standalone binary embeds its inherit-flattened
guardfile, so it is self-contained in-container.

There is no AWS in an explore container, so the write guardfile **overrides** the
inherited SSM auth with `value env "FORGEJO_TOKEN"` (`auth` is a singleton, so the
override is write-tier-only; read/admin stay SSM for host use) - exactly the token
the daemon holds and seeds.

> The generated `docs/ward-kdl/ward-kdl.forgejo.write.guardfile.md` still names the
> read-tier SSM auth in its header (the renderer reads the inheritance root); the
> compiled binary uses the env auth. The guardfile is the source of truth.

## Lifecycle (entrypoint)

Started **as root, before the privilege-drop**, gated on `WARD_READONLY`:

1. `install_ward_kdl_write` downloads `ward-kdl-write-linux-<arch>` from the same
   release `ward` came from (best-effort; a miss leaves the broker unstarted).
2. `start_broker` runs the daemon, waits for the socket, then exports
   `WARD_BROKER_SOCK` so the dropped agent inherits it.

The release publishes the tier binary via `publish-kdl-write`
(`.forgejo/workflows/release.yml`): amd64 through the generator, arm64 cross-built
from the module the generator materializes in its cache.

## Dual mode (not a cutover)

Additive: `FORGEJO_TOKEN` is **still** present and clients are **not** rewired.
The broker rides alongside. Unit C rewires clients to the socket; Unit D drops the
raw token. Any broker-path failure falls back to the token path - it never blocks a
launch.

## See also

- `cli-guard/pkg/broker` - the policy core (cli-guard#167).
- [docs/agent-explore.md](agent-explore.md) - the session this hardens.
- [docs/ward-kdl.md](ward-kdl.md) - the tier binaries the executor shells.
