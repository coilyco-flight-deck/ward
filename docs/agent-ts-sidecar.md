---
doc_goal: Let an operator route a Docker Desktop run to the tailnet-only ollama tower by attaching to the standing ansible-owned SOCKS5 box, and grasp why the by-name, capability-free, no-mint design is the safe non-blocking slice - including how to preflight it, dial the tower as localhost, and read its current validation status.
---
# ward agent: the sidecar mechanism (Docker Desktop tailnet route)

The **sidecar** is the Docker Desktop mechanism `--tailnet` auto-selects: it reaches
a tailnet-only host like the Ollama tower (`<ollama-host>:11434`)
from a run whose docker daemon runs in a LinuxKit VM that is **not** a tailnet node. The
host-net sibling is in [agent-host-net.md](agent-host-net.md). One current
implementation uses the `ward-tailnet` / `mac-proxy` deployment described in
[agent-tailnet-topology.md](agent-tailnet-topology.md), but the ward contract stays
generic: a run gets a SOCKS5 URL and a tower URL, not a product-level dependency on
Kai's infra shape.

## What it does

A run **attaches to a standing, shared mac-proxy SOCKS5 box** over a known docker
network instead of minting its own per-run tailscale sidecar. The contract is two
fixed, ansible-owned names (not personal): `ward-tailnet` and `mac-proxy`.

- **The standing box** runs once (`restart: unless-stopped`), serving SOCKS5 on a
  known `host:port`. Ward never converges that box. It only **attaches and
  preflights**.
- **The run joins a configured tailnet network** (`--network=$WARD_TAILNET_NETWORK`),
  a **user-defined** network so it resolves the box **by name**. Routing is
  **per-connection**, never `ALL_PROXY`.
- **No keys, no SSM of its own.** Reached by name, a run mints no node, injects no
  `TS_AUTHKEY`, fetches nothing from SSM for the sidecar. The mechanism needs no SSM, but
  `--tailnet` still mounts `~/.aws` on both routes.
- **Route:** `WARD_TS_SOCKS5` is a `socks5h://host:port` URL and
  `WARD_TOWER_OLLAMA` is `http://<ollama-host>:11434`, both plain. `socks5h` hands
  the hostname to the proxy to resolve **tailnet side**, so the run dials by name.

## Preflight

Before the image pull and stale-container sweep - not after them, so a doomed run
burns nothing ([ward#597](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/597)) - a sidecar run readies the tailnet:
`docker network inspect` reads which containers are attached, and a missing network is a
**provisioning step, not a launch failure**. The two conditions are handled distinctly
so the operator is never left guessing:

- **Network absent** (the inspect fails, e.g. a fresh Docker Desktop host that never
  converged the network): ward **creates it** and launches. Only if the create itself
  cannot land - the daemon is unreachable or refuses it - does ward error with a
  creation hint and the `--no-tailnet` fallback. This replaces the old hard network
  miss failure and stops docker from 125ing the run mid-launch with a bare exit that
  names no cause ([ward#597](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/597)).
- **Box unattached** (the network exists - or ward just created it - but the configured
  SOCKS5 box is not on it): ward **warns and launches anyway**. That box is owned by
  local infra, not ward, so this is a live-observe gap, not a launch blocker: the
  container runs, but the tailnet SOCKS5 route will not resolve until that deployment
  converges on this host. A freshly-created network always hits this warning until that
  role runs.

There is no per-run mint or teardown. This ready-up runs on **every** sidecar dispatch
path, including the advisor's `docker create`-direct research path that never touches
the shared launch helper.

## Dial the tower from inside a run

A sidecar run backgrounds a **userspace loopback forwarder** (`ward container forward`,
torn down with the run). It listens on `127.0.0.1:11434` and bridges each connection to
`<ollama-host>:11434` through the box over `$WARD_TS_SOCKS5` (`socks5h`, resolved
tailnet-side). The tower **is** localhost, dial it with **no `--proxy`**:

```bash
curl "$WARD_TOWER_OLLAMA_LOCAL/api/tags"   # WARD_TOWER_OLLAMA_LOCAL=http://localhost:11434
```

It needs **no container capability** (no `NET_ADMIN`, `/dev/net/tun`, or `ALL_PROXY`), the
non-blocking slice of the full-tunnel epic. Bundled clients default to
`localhost:11434` (opencode via `WARD_OLLAMA_URL`, goose's `OLLAMA_HOST`), so model
calls **auto-route** to the tower with no per-client config.

The explicit per-request proxy path stays valid, `WARD_TS_SOCKS5` +
`WARD_TOWER_OLLAMA` are plain: `curl --proxy "$WARD_TS_SOCKS5" "$WARD_TOWER_OLLAMA/api/tags"`.
If you need SSH reachability or proxy recipes, keep that in local infrastructure
or operator docs. Ward only carries the generic proxy env.

## Use it

The sidecar is no longer a separate flag: it is one value of `--tailnet-mode`,
so it is not "mutually exclusive" with a host-net flag, `--tailnet` picks one mechanism
per run. On Docker Desktop plain `--tailnet` auto-selects it, `--tailnet-mode sidecar`
forces it anywhere:

```bash
warded engineer coilyco-flight-deck/agent-proxy#1 --tailnet --tailnet-mode sidecar
```

`--print` shows the preflight `docker network inspect` line and `--network=ward-tailnet`.

## Validation status

The ACL hop `tag:proxy -> tag:<ollama-host>:11434` is merged, and the current
implementation is converged by local infra. The ward wiring (attach, preflight,
by-name route, forwarder bridge) is **unit-tested**, a live end-to-end lands as a
follow-up once the box is converged.

## See also

- [agent-host-net.md](agent-host-net.md) - the host-route sibling for a tailnet host.
- [agent-flags.md](agent-flags.md) - the launch flag list.
