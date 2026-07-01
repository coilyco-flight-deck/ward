# Root credential broker (ward side)

The **root credential broker** hardens the [director's surface](agent-surface.md):
the session would otherwise keep `FORGEJO_TOKEN` in the agent's env, the bot token a
push rebuilds from (ward#318). The broker closes that gap - a **root daemon** holds
it; the dropped agent reaches the forge through a socket.

This is the **ward side** (ward#329 Unit B). The **policy core** - protocol,
authorizer, executor interface, server - lives in `cli-guard/pkg/broker`
(cli-guard#167): policy in cli-guard, glue + credential in ward.

## The pieces

- **`ward container broker`** (`cmd/ward/broker.go`) is the daemon's `main`: it
  resolves the root-held `FORGEJO_TOKEN`, opens the socket and permissions it
  `root:<agent-gid>` mode `0660` (group-readable, never world), then serves until a
  signal cancels it. `broker.Server` only listens on an already-permissioned socket.
- **The executor** (`cmd/ward/broker_exec.go`) shells `ward-kdl-write ops forgejo
  <verb>` for **file / edit / comment issue**, seeding the bot token into the env.
- **The authorizer** is the write tier: the file/edit/comment/`dispatch` op
  allowlist + `broker.Policy`'s invariants + a `coily*` owner gate. Delete/admin
  and every other op refuse out-of-tier before the executor runs.

## ward-kdl-write + auth

The executor shells the **write tier** (ward#240): `read + create/edit`, delete
absent at compile time; the standalone binary embeds its inherit-flattened
guardfile. With no AWS in an explore container, the write guardfile **overrides**
the inherited SSM auth with `value env "FORGEJO_TOKEN"` (write-tier only; read/admin
stay SSM) - the token the daemon holds. The generated guardfile doc still names the
SSM auth in its header; the compiled binary uses the env auth.

## Lifecycle (entrypoint)

Started **as root, before the privilege-drop**, gated on `WARD_READONLY`:

1. `install_ward_kdl_write` downloads `ward-kdl-write-linux-<arch>` from the same
   release `ward` came from (best-effort; a miss leaves the broker unstarted).
2. `start_broker` runs the daemon, waits for the socket, exports `WARD_BROKER_SOCK`,
   and sends fd 1+2 to `WARD_BROKER_LOG` (default `/run/ward/broker.log`), never the
   shared TTY - a raw per-op line would corrupt the director's Claude Code TUI (ward#389).

The release publishes the tier binaries via the `publish-kdl-tiers` matrix job; the
broker downloads `write`.

## Routing the clients (Unit C)

`cmd/ward/broker_client.go` is one shared `broker.Client` wrapper both chokepoints
route through when `WARD_BROKER_SOCK` is set:

1. **`ops forgejo <verb>`** (`ops.go`): the specverb `Wrap` classifies by the leaf's
   `verb.Spec.Name` tail. Issue create/edit/comment/close/reopen forward as the
   matching `broker.Op`; reads + `--dry-run` go direct; other mutations refuse locally.
2. **`warded #N` dispatch** (`resolveForgejoToken`): the child env-file's token is
   seeded from the broker's `dispatch #N` response, not a token the agent holds.

## Dual mode (not a cutover)

`FORGEJO_TOKEN` is **still** present alongside the broker. Unit C rewires the
clients; Unit D drops the raw token. A dispatch-seed failure falls back to env->SSM.

## Not the dispatch broker (ward#382)

Two brokers share the name. **This credential broker** is an in-container **unix
socket** (`/run/ward/broker.sock`). The **dispatch broker** (`agent_dispatch_broker.go`)
launches runs over **TCP on the docker gateway** (`WARD_DISPATCH_BROKER_ADDR`, a
per-launch token) - not a bind-mount, which lands as an **empty dir** under Docker
Desktop / linuxkit (ward#382, ward#391; see [agent-surface.md](agent-surface.md)).
Dialing this socket from a dispatch client answers `unsupported protocol version`,
a `wrong broker` hint.

## See also

- `cli-guard/pkg/broker` - the policy core (cli-guard#167).
- [docs/agent-surface.md](agent-surface.md) - the read-only surface this hardens.
- [docs/ward-kdl.md](ward-kdl.md) - the tier binaries the executor shells.
