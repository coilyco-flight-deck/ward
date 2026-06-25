# ward agent: --host-net (tailnet route)

`--host-net` is the **opt-in network escalation** (ward#330): join a carry to the
host network namespace (`docker run --network=host`) so it inherits the host's
`tailscale0` + MagicDNS and can reach tailnet-only hosts like `kai-tower-3026`.

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

On **Docker Desktop** (macOS/Windows) the daemon runs inside a **LinuxKit VM**;
tailscale runs on the Mac/Windows host one layer up. `--network=host` joins the
carry to the **VM's** netns, which is **not** a tailnet node: no `tailscale0`, no
MagicDNS, so tailnet names (`api`, `kai-tower-3026`) do not resolve and
`--host-net` is a **no-op for tailnet access** however the host is configured. A
documented Tailscale + Docker Desktop limitation, not a ward bug - it confines
`--host-net` to native-Linux tailnet hosts.

ward **detects and warns**: when `--host-net` is set but the route is unlikely to
reach the tailnet - a non-Linux host (Docker Desktop), or Linux with no
`tailscale0` in the joined netns - it prints a loud `WARNING:` at launch so a
no-op route never reads as success. The carry still launches; the warning just
says the tailnet route will not be there.

Even on native Linux a container often still needs `100.100.100.100` added to its
`/etc/resolv.conf`: container DNS does not inherit the host's per-link
systemd-resolved MagicDNS config
([tailscale/tailscale#14467](https://github.com/tailscale/tailscale/issues/14467)).

## The cross-platform answer: in-container tailscale sidecar (scope only)

The **portable** route - and the **only** one on Docker Desktop - is an
in-container `tailscaled`: run it inside the container (ephemeral tagged auth key),
or as a sidecar the app shares the netns of (`network_mode: service:tailscale`), so
the carry is its own tailnet node regardless of the docker host's kernel. See
Tailscale's [Docker Desktop](https://tailscale.com/docs/features/containers/docker/docker-desktop)
and [connect-a-container](https://tailscale.com/docs/features/containers/docker/how-to/connect-docker-container) guides.

**Scope/spec only, not shipped.** Standing it up needs an ephemeral tagged auth
key, a node tag, and an ACL grant for that tag - a real secret-handling decision
that is **human-gated** and must not be picked by a headless run (the "no auth key,
no minting" line ward#330 drew). Until then, `--host-net` on a native-Linux tailnet
host is the only path that ships.

## Wiring

A shared `hostNetFlag()` on the four container-spinning surfaces threads
`upPlan.HostNet` into `--network=host` from `dockerArgvHead` (shows in `--print`);
the warning fires from `createAgentContainer`, the shared launch point.

## Off by default, and it implies `--aws`

`--host-net` **widens isolation** to the host's full network view, so it is opt-in.
The tower's FQDN is SSM-only (never hardcoded), and a route with no resolver is
useless - so `--host-net` **implies the `~/.aws` mount** `--aws` adds. A tower carry:

```bash
warded work coilyco-flight-deck/agent-proxy#1 --host-net
```

resolves the FQDN from SSM and reaches `http://$TOWER:11434` inside the container.

## See also

- [container.md](container.md) - the least-access model this widens.
- [agent-flags.md](agent-flags.md) - the launch flag list.
- [agent-credentials.md](agent-credentials.md) - how routes + creds are seeded.
