# ward agent: --ts-sidecar (Docker Desktop tailnet route)

`--ts-sidecar` is the **Docker Desktop sibling of `--host-net`** (ward#333, by-name
reach ward#337, standing-box attach ward#349): it reaches a tailnet-only host like
the Ollama tower (`kai-tower-3026:11434`) from a carry whose docker daemon runs in a
LinuxKit VM that is **not** a tailnet node, where `--host-net` can't (ward#332).

## What it does (ward#349)

A carry **attaches to a standing, shared mac-proxy SOCKS5 box** over a known docker
network instead of minting its own per-run tailscale sidecar. This is the ward half
of agentic-os#291; the infra sibling owns the box's compose stack. The shared
contract is two names: the docker network `ward-tailnet` and the proxy `mac-proxy`.

- **The standing box** runs once, `restart: unless-stopped`, serving SOCKS5 on
  `0.0.0.0:1055` - to Mac-host tools via published `127.0.0.1:1055` and to carries
  via `ward-tailnet`. It is its own tailnet node (`mac-proxy`, `tag:proxy`). **ward
  never converges it** - that stays in ansible; ward only **attaches and preflights**.
- **The carry joins `ward-tailnet`** (`--network=ward-tailnet`), a **user-defined**
  network so the carry resolves the box **by name** (`mac-proxy:1055`); the default
  bridge has no name resolution. Routing is **per-connection**, never `ALL_PROXY`.
- **No keys, no SSM.** The box is standing and reached by name, so a carry mints no
  node, injects no `TS_AUTHKEY`, and fetches nothing from SSM. It is the first
  network route that does **not** imply `--aws`.
- **Route (ward#337):** the tower is dialed by MagicDNS name through the proxy - no
  SSM IP lookup. `WARD_TS_SOCKS5` is `socks5h://mac-proxy:1055` and `WARD_TOWER_OLLAMA`
  is `http://kai-tower-3026:11434`, both plain. `socks5h` hands the hostname to the
  proxy to resolve **tailnet side**, which is what lets the carry dial by name.

## Preflight (ward#349)

Before attaching, a carry verifies the box is reachable: `docker network inspect
ward-tailnet` must succeed (the network exists) **and** `mac-proxy` must be attached.
On failure ward launches nothing and errors:

```
ward container: standing tailnet proxy not found - converge the mac-proxy infra role (agentic-os#291)
```

There is no per-run mint or teardown, and no `<carry>-ts` container is ever created.

## Dial the tower from inside a carry

```bash
curl --proxy "$WARD_TS_SOCKS5" "$WARD_TOWER_OLLAMA/api/tags"
```

`--proxy` routes just that request through the box and lets tailscaled resolve
`kai-tower-3026` tailnet-side. Auto-wiring goose/qwen over the proxy is a follow-up.

## Use it

`--ts-sidecar` is **mutually exclusive with `--host-net`**, and unlike it **does not
imply `--aws`** (it needs no SSM):

```bash
warded engineer coilyco-flight-deck/agent-proxy#1 --watch --ts-sidecar
```

`--print` shows the preflight `docker network inspect` line and the carry's
`--network=ward-tailnet`. The flag spelling is retained for compatibility; the help
text now reads "attach to the standing tailnet proxy".

## Validation status

The ACL hop `tag:proxy -> tag:kai-tower-3026:11434` (infrastructure#400) is merged.
The standing-box model (the `ward-tailnet` network + `mac-proxy` compose stack) is
converged by the infra sibling of agentic-os#291. The ward wiring here - attach,
preflight, by-name route - is **unit-tested**; a live end-to-end needs the converged
box, so it lands as a follow-up. This run mints no keys and runs no terraform.

## See also

- [agent-host-net.md](agent-host-net.md) - the host-route sibling for a tailnet host.
- [container.md](container.md) - the least-access network model.
- [agent-flags.md](agent-flags.md) - the launch flag list.
