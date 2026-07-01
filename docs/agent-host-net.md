# ward agent: tailnet route (--tailnet)

`--tailnet` is the **opt-in network escalation** (ward#330, consolidated ward#362):
one user flag that reaches tailnet-only hosts like `kai-tower-3026`. It **auto-selects
the mechanism by platform** - the host-network route on native Linux, the SOCKS5
sidecar on Docker Desktop (where the host VM is not a tailnet node) - and implies
`--aws` on both. The hidden `--tailnet-mode auto|host-net|sidecar` pins the mechanism.

This doc covers the **host-net** mechanism (the **sidecar** is in
[agent-ts-sidecar.md](agent-ts-sidecar.md)): joining a carry to the host network
namespace (`docker run --network=host`) so it inherits the host's `tailscale0` + MagicDNS.

## Why it exists

The least-access default ([container.md](container.md)) lands a carry on docker's
bridge: public internet, but **not** the host's tailnet-only hosts. `kai-tower-3026`
serves ollama over the tailnet at an SSM-held FQDN, so a carry can't live-test it.

## What it does, and where it works

`--network=host` joins the container to the **docker daemon's** network namespace.
That netns is a tailnet node - so the carry inherits `tailscale0` + MagicDNS and
reaches the tower directly, no in-container `tailscaled`/auth key/minting - **only
on a native-Linux host that is itself on the tailnet**.

## It does nothing for the tailnet on Docker Desktop (ward#332)

On **Docker Desktop** (macOS/Windows) the daemon runs inside a **LinuxKit VM**, so
`--network=host` joins the carry to the **VM's** netns, which is **not** a tailnet node:
no `tailscale0`, no MagicDNS, so tailnet names do not resolve and host-net is a **no-op
for tailnet access**. A documented Tailscale + Docker Desktop limitation, not a ward bug,
which is why `--tailnet` auto-selects the sidecar there instead.

ward **detects and warns**: when host-net is chosen but unlikely to reach the tailnet (a
non-Linux host, or Linux with no `tailscale0` in the joined netns) it prints a loud
`WARNING:` at launch so a no-op route never reads as success. The carry still launches.

Even on native Linux a container often needs `100.100.100.100` added to its
`/etc/resolv.conf`: container DNS does not inherit the host's per-link systemd-resolved
MagicDNS config ([tailscale/tailscale#14467](https://github.com/tailscale/tailscale/issues/14467)).

The portable route, and the only one on Docker Desktop, is the **sidecar**: a SOCKS5
proxy that **is** a tailnet node, which `--tailnet` auto-selects there (ward#349). See
[agent-ts-sidecar.md](agent-ts-sidecar.md).

## Wiring

A shared `tailnetFlags()` on the four container-spinning surfaces registers
`--tailnet` + the hidden `--tailnet-mode`. `resolveTailnet` picks the mechanism by
platform (ward#362), and the host-net choice threads `upPlan.HostNet` into
`--network=host` from `dockerArgvHead` (shows in `--print`). The warning fires from
`createAgentContainer`, the shared launch point.

## Off by default, and it implies `--aws`

`--tailnet` **widens isolation** to the host's network view, so it is opt-in.
The tower's FQDN is SSM-only (never hardcoded), and a route with no resolver is
useless - so `--tailnet` **implies the `~/.aws` mount** `--aws` adds, on both
mechanisms (ward#362). A tower carry pinned to the host-net route:

```bash
warded engineer coilyco-flight-deck/agent-proxy#1 --tailnet --tailnet-mode host-net
```

resolves the FQDN from SSM and reaches `http://$TOWER:11434` inside the container.
Plain `--tailnet` picks host-net automatically on native Linux.

## See also

- [agent-ts-sidecar.md](agent-ts-sidecar.md) - the Docker Desktop sibling route.
- [container.md](container.md) - the least-access model this widens.
- [agent-flags.md](agent-flags.md) - the launch flag list.
- [agent-credentials.md](agent-credentials.md) - how routes + creds are seeded.
