# ward agent: --ts-sidecar (Docker Desktop tailnet route)

`--ts-sidecar` is the **Docker Desktop sibling of `--host-net`** (ward#333): it
reaches a tailnet-only host like the Ollama tower (`kai-tower-3026:11434`) from a
carry whose docker daemon runs in a LinuxKit VM that is **not** a tailnet node,
where `--host-net` can't (ward#332).

## What it does

It mirrors the proven `tooling-tailscale` userspace SOCKS5 pattern - it does not
invent a new shape:

- **A userspace tailscale sidecar** runs next to the carry from
  `tailscale/tailscale:latest` with `TS_USERSPACE=true` and
  `TS_SOCKS5_SERVER=127.0.0.1:1055`. Userspace mode means **no `/dev/net/tun`, no
  `NET_ADMIN`, no host route** - required because the daemon is in a VM. The
  sidecar is its own tailnet node (hostname `mac-proxy`, `tag:proxy`).
- **The carry joins the sidecar's netns** (`--network=container:<carry>-ts`), so
  `127.0.0.1:1055` reaches the SOCKS5 proxy and the carry keeps the sidecar's
  bridge for ordinary public egress. Routing is **per-connection**: only what you
  send through `:1055` reaches the tailnet, so there is no host-wide `ALL_PROXY`
  (the proxy carries the tailnet, not public egress).
- **Auth key:** the reusable + ephemeral `tag:proxy` key at
  `/coilysiren/mac-proxy/ts-authkey` is resolved host-side, injected as
  `TS_AUTHKEY` through a private env-file, and **never written to disk** or argv.
- **Route:** the tower is dialed by **tailnet IP** (resolved from
  `/coilysiren/kai-tower-3026/tailnet-ip`) on `:11434` through the proxy. Dialing
  by IP skips MagicDNS through the proxy, dodging a class of resolver confusion
  (sharp-edges doctrine). The carry is told the proxy in `WARD_TS_SOCKS5` and the
  tower endpoint (base64'd, SSM-held) in `WARD_TOWER_OLLAMA_B64`.

## Use it

Like `--host-net`, `--ts-sidecar` **implies `--aws`** (the auth key and tower IP
are SSM-only) and the two are **mutually exclusive** - pick the one for your host:

```bash
# Docker Desktop (host VM is not a tailnet node): the sidecar route.
warded work coilyco-flight-deck/agent-proxy#1 --ts-sidecar
```

`--print` shows both the sidecar `docker run` and the carry's
`--network=container:<carry>-ts`. An attached carry tears its sidecar down on
exit; a detached carry's sidecar is reclaimed by the next launch's orphan sweep
(a sidecar whose carry is gone).

## End-to-end reach awaits the ACL grant

The hop `tag:proxy -> tag:kai-tower-3026:11434` is added in infrastructure#400 and
must be applied (operator-run `ward terraform-tailscale action=apply`) before a
`--ts-sidecar` carry can actually talk to the tower. The wiring - sidecar, key
injection, SOCKS5 route - ships here; live validation is deferred until that apply
lands. This run mints no keys, runs no terraform, and fetches no tailscale admin
creds.

## See also

- [agent-host-net.md](agent-host-net.md) - the host-route sibling for a tailnet host.
- [container.md](container.md) - the least-access network model.
- [agent-flags.md](agent-flags.md) - the launch flag list.
