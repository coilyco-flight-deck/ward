# ward agent: --ts-sidecar (Docker Desktop tailnet route)

`--ts-sidecar` is the **Docker Desktop sibling of `--host-net`** (ward#333, by-name
reach in ward#337): it reaches a tailnet-only host like the Ollama tower
(`kai-tower-3026:11434`) from a carry whose docker daemon runs in a LinuxKit VM
that is **not** a tailnet node, where `--host-net` can't (ward#332).

## What it does

It mirrors the proven `tooling-tailscale` userspace SOCKS5 pattern - it does not
invent a new shape:

- **A userspace tailscale sidecar** runs next to the carry from
  `tailscale/tailscale:latest` with `TS_USERSPACE=true` and
  `TS_SOCKS5_SERVER=127.0.0.1:1055`. Userspace mode means **no `/dev/net/tun`, no
  `NET_ADMIN`, no host route** (the daemon is in a VM). The sidecar is its own
  tailnet node (hostname `mac-proxy`, `tag:proxy`).
- **The carry joins the sidecar's netns** (`--network=container:<carry>-ts`), so
  `127.0.0.1:1055` reaches the SOCKS5 proxy. Routing is **per-connection**: only
  what you send through `:1055` reaches the tailnet, never a host-wide `ALL_PROXY`.
- **Auth key:** the reusable + ephemeral `tag:proxy` key at
  `/coilysiren/mac-proxy/ts-authkey` is resolved host-side, injected as
  `TS_AUTHKEY` through a private env-file, and **never written to disk** or argv.
  It is the **only** SSM dependency a `--ts-sidecar` carry has (ward#337).
- **Route (ward#337):** the tower is dialed by its **MagicDNS name**
  `http://kai-tower-3026:11434` through the proxy - **no SSM IP lookup**. The carry
  is told the proxy in `WARD_TS_SOCKS5` and the endpoint in `WARD_TOWER_OLLAMA`,
  both plain (a MagicDNS name is not a secret). `WARD_TS_SOCKS5` is **`socks5h://`**
  (not `socks5://`): the `h` hands the hostname to the proxy to resolve **tailnet
  side**, where MagicDNS lives - what lets the carry dial by name. That stays
  consistent with the `tooling-tailscale` "skip MagicDNS" sharp edge, which warns
  against the carry's **local** resolver chasing a name it can't resolve; with
  `socks5h` the local resolver never sees it.

## Dial the tower from inside a carry

`WARD_TS_SOCKS5` and `WARD_TOWER_OLLAMA` are plain in the agent's env, so a carry
reaches the tower's Ollama per-connection (never a process-wide proxy):

```bash
curl --proxy "$WARD_TS_SOCKS5" "$WARD_TOWER_OLLAMA/api/tags"
```

`--proxy` routes just that request through the sidecar and lets tailscaled resolve
`kai-tower-3026` tailnet-side. Auto-wiring goose/qwen at the tower over the sidecar
is a follow-up (needs a per-connection proxy or loopback forwarder, not `ALL_PROXY`).

## Use it

Like `--host-net`, `--ts-sidecar` **implies `--aws`** (the auth key is SSM-only)
and the two are **mutually exclusive** - pick the one for your host:

```bash
# Docker Desktop (host VM is not a tailnet node): the sidecar route.
warded engineer coilyco-flight-deck/agent-proxy#1 --watch --ts-sidecar
```

`--print` shows both the sidecar `docker run` and the carry's
`--network=container:<carry>-ts`. An attached carry tears its sidecar down on
exit; a detached carry's sidecar is reclaimed by the next launch's orphan sweep
(a sidecar whose carry is gone).

## Validation status

The ACL hop `tag:proxy -> tag:kai-tower-3026:11434` (infrastructure#400, `dab4b51`)
is **merged and applied**, and the `tag:proxy` auth key already lives at
`/coilysiren/mac-proxy/ts-authkey`, so the human-gated preconditions #332 drew the
line at are satisfied. The wiring here - sidecar, key injection, by-name SOCKS5
route - is **unit-tested only**; a full live end-to-end (a carry actually curling
the tower's Ollama) needs this new ward version plus the key in hand, so it lands
as a follow-up run on the new ward (ward#337). This run mints no keys, runs no
terraform, and fetches no tailscale admin creds.

## See also

- [agent-host-net.md](agent-host-net.md) - the host-route sibling for a tailnet host.
- [container.md](container.md) - the least-access network model.
- [agent-flags.md](agent-flags.md) - the launch flag list.
