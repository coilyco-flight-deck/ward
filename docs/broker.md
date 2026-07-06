---
doc_goal: Explain ward's root credential broker as a real privilege-drop security boundary - a root daemon holding FORGEJO_TOKEN so the dropped agent reaches the forge only through a permissioned socket - and let a maintainer trace its pieces, lifecycle, and its separation from the dispatch broker.
---
# Root credential broker (ward side)

The **root credential broker** hardens the [director's surface](agent-surface.md):
the session would otherwise keep `FORGEJO_TOKEN` in the agent's env. The
broker closes that gap - a **root daemon** holds it; the dropped agent reaches the
forge through a socket.

## The A/B/C/D build-out

The broker landed in four staged units, and the rest of this doc leans on those
labels, so pin them down first:

- **Unit A** - the **policy core** in `cli-guard/pkg/broker`: protocol,
  authorizer, executor interface, server. Policy lives in cli-guard.
- **Unit B** - the **ward side** ([ward#329](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/329)): the root daemon `main`, the
  socket lifecycle, and the executor that seeds the ward-held credential. Glue +
  credential in ward. This doc is mostly Unit B.
- **Unit C** - **routing the clients** ([ward#334](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/334)): rewiring the two
  chokepoints (`ops forgejo` mutations and `warded #N` dispatch) to reach the
  forge through the socket instead of a token in the agent's env.
- **Unit D** - **dropping the raw token** ([ward#608](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/608)):
  removing `FORGEJO_TOKEN` from the dropped agent's env. **Still deferred, and not a
  ward-only change** - it is blocked on a cli-guard **read op** first (see "Dual
  mode" below). Do not scrub the token before reads route through the broker, or
  explore sessions go blind.

This is the **ward side** (Unit B). Policy in cli-guard, glue + credential in ward.

## How the pieces relate at runtime

A **root daemon** holds the token and listens on the group-readable socket. When
a dropped agent runs a forge write, its client (Unit C) sends an **op** over that
socket. The daemon's **authorizer** decides whether the op is in-tier, and only
then does the **executor** shell the write-tier CLI with the token seeded into
its env. The agent never touches the token - it only ever holds a socket handle.

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

The executor shells the **write tier**: `read + create/edit`, delete
absent at compile time; the standalone binary embeds its inherit-flattened
guardfile. With no AWS in an explore container, the write guardfile **overrides**
the inherited SSM auth with `value env "FORGEJO_TOKEN"` (write-tier only; read/admin
stay SSM) - the token the daemon holds. The generated guardfile doc still names the
SSM auth in its header; the compiled binary uses the env auth.

## Lifecycle (entrypoint)

Started **as root, before the privilege-drop**, gated on `WARD_READONLY`:

1. `install_ward_kdl_write` downloads `ward-kdl-write-linux-<arch>` from the
   **internal package channel** - the Forgejo generic package registry
   (`generic/ward-kdl-write/<tag>/...`), keyed to the release tag, **not** the
   release page. The tier release assets were dropped; `publish-kdl-write`
   publishes the write tier here instead
   ([ward#501](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/501)).
   A miss falls back to the `FORGEJO_TOKEN` path ("Dual mode" below).
2. `start_broker` runs the daemon, waits for the socket, exports `WARD_BROKER_SOCK`,
   and sends fd 1+2 to `WARD_BROKER_LOG` (default `/run/ward/broker.log`), never the
   shared TTY - a raw per-op line would corrupt the director's Claude Code TUI.

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

**Why Unit D is not just a scrub** ([ward#608](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/608)):
forge **reads** (`get` / `list` / `search` / `view`) still go **direct** - the
client's `forgejoReadVerbs` sends them to `ward ops forgejo`, which resolves auth
from env `FORGEJO_TOKEN`. `ward-kdl-read` can't cover them (its guardfile auths
from SSM, and an explore box has no AWS), and the broker can't serve them
(`cli-guard/pkg/broker` has no read op - only file/edit/comment/dispatch). So the
raw-token scrub is blocked on a **cli-guard read op** landing first; only once
reads round-trip through the broker can Unit D remove the token without blinding
explore sessions.

## Not the dispatch broker

Two brokers share the name. **This credential broker** is an in-container **unix
socket** (`/run/ward/broker.sock`). The **dispatch broker** (`agent_dispatch_broker.go`)
launches runs over **TCP on the docker gateway** (`WARD_DISPATCH_BROKER_ADDR`, a
per-launch token), not a bind-mount (see [agent-surface.md](agent-surface.md)).
Dialing this socket from a dispatch client answers `unsupported protocol version`.

### Diagnosing a dispatch broker `get issue` failure ([ward#596](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/596))

The dispatch broker resolves the issue by shelling to `ward ops forgejo issue get`,
whose auth resolves through `forgejoTokenResolver` (env `FORGEJO_TOKEN`, else SSM).
When neither is available on the **host** the leaf exits `4` (cli-guard `Internal`,
the auth value-chain), distinct from an HTTP non-2xx (`3`). That subprocess stderr
names the cause, so `forgejoClient.run` now folds it into the returned error rather
than leaking a bare `exit status 4` - the surface reads the reason without the run log.

## See also

- `cli-guard/pkg/broker` - the policy core.
- [docs/agent-surface.md](agent-surface.md) - the read-only surface this hardens.
- [docs/ward-kdl.md](ward-kdl.md) - the tier binaries the executor shells.
- [docs/forgejo-token-audit.md](forgejo-token-audit.md) - the audited raw-token read sites the resolver chokepoints funnel through.
