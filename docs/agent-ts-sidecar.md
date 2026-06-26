# ward agent: --ts-sidecar (Docker Desktop tailnet route)

`--ts-sidecar` is the **Docker Desktop sibling of `--host-net`** (ward#333, by-name
reach ward#337, standing-box attach ward#349): it reaches a tailnet-only host like
the Ollama tower (`kai-tower-3026:11434`) from a carry whose docker daemon runs in a
LinuxKit VM that is **not** a tailnet node.

## What it does (ward#349)

A carry **attaches to a standing, shared mac-proxy SOCKS5 box** over a known docker
network instead of minting its own per-run tailscale sidecar (the ward half of
agentic-os#291). The shared contract is two names: the docker network
`ward-tailnet` and the proxy `mac-proxy`.

- **The standing box** runs once (`restart: unless-stopped`), serving SOCKS5 on
  `0.0.0.0:1055`. It is its own tailnet node (`mac-proxy`, `tag:proxy`); **ward
  never converges it** (ansible owns that), ward only **attaches and preflights**.
- **The carry joins `ward-tailnet`** (`--network=ward-tailnet`), a **user-defined**
  network so it resolves the box **by name** (`mac-proxy:1055`). Routing is
  **per-connection**, never `ALL_PROXY`.
- **No keys, no SSM.** Reached by name, a carry mints no node, injects no
  `TS_AUTHKEY`, fetches nothing from SSM - the first route that **doesn't** imply `--aws`.
- **Route (ward#337):** `WARD_TS_SOCKS5` is `socks5h://mac-proxy:1055` and
  `WARD_TOWER_OLLAMA` is `http://kai-tower-3026:11434`, both plain; `socks5h` hands
  the hostname to the proxy to resolve **tailnet side**, so the carry dials by name.

## Preflight (ward#349)

Before attaching, a carry verifies the box is reachable: `docker network inspect
ward-tailnet` must succeed **and** `mac-proxy` must be attached. On failure ward
launches nothing and errors `standing tailnet proxy not found - converge the
mac-proxy infra role (agentic-os#291)`. There is no per-run mint or teardown.

## Dial the tower from inside a carry (ward#359)

A `--ts-sidecar` carry backgrounds a **userspace loopback forwarder** (`ward
container forward`, torn down with the carry on the reaper's EXIT trap). It listens
on `127.0.0.1:11434` and bridges each connection to `kai-tower-3026:11434` through
the box over `$WARD_TS_SOCKS5` (`socks5h`: the host rides as a domain name, resolved
tailnet-side). The tower **is** localhost - dial it with **no `--proxy`**:

```bash
curl "$WARD_TOWER_OLLAMA_LOCAL/api/tags"   # WARD_TOWER_OLLAMA_LOCAL=http://localhost:11434
```

It needs **no container capability** (no `NET_ADMIN`, no `/dev/net/tun`, no
`ALL_PROXY`) - the non-blocking slice of the full-tunnel epic (infrastructure#411,
which tracks the cap-requiring TUN proxifier). The bundled clients already default
to `localhost:11434` (qwen/opencode via `WARD_OLLAMA_URL`, goose's `OLLAMA_HOST`),
so model calls **auto-route** to the tower with no per-client config.

The explicit per-request proxy path stays valid; both `WARD_TS_SOCKS5` and
`WARD_TOWER_OLLAMA` remain plain in the agent's env:

```bash
curl --proxy "$WARD_TS_SOCKS5" "$WARD_TOWER_OLLAMA/api/tags"
```

## Use it

`--ts-sidecar` is **mutually exclusive with `--host-net`**, and unlike it **does not
imply `--aws`** (it needs no SSM):

```bash
warded engineer coilyco-flight-deck/agent-proxy#1 --ts-sidecar
```

`--print` shows the preflight `docker network inspect` line and `--network=ward-tailnet`.

## Validation status

The ACL hop `tag:proxy -> tag:kai-tower-3026:11434` (infrastructure#400) is merged;
the standing-box model is converged by the infra sibling of agentic-os#291. The
ward wiring - attach, preflight, by-name route, the forwarder bridge - is
**unit-tested**; a live end-to-end needs the converged box, so it lands as a follow-up.

## See also

- [agent-host-net.md](agent-host-net.md) - the host-route sibling for a tailnet host.
- [agent-flags.md](agent-flags.md) - the launch flag list.
