# ward agent: --host-net (tailnet route)

`--host-net` is the **opt-in network escalation** (ward#330): join a carry to the
host network namespace (`docker run --network=host`) so it inherits the host's
`tailscale0` + MagicDNS and can reach tailnet-only hosts like `kai-tower-3026`.

## Why it exists

The least-access default ([container.md](container.md)) lands a carry on docker's
bridge network: it reaches the public internet but **not** the host's
tailnet-only hosts. `kai-tower-3026` serves ollama over the tailnet at an
SSM-held FQDN (`/coilysiren/kai-tower-3026/tailnet-fqdn`), so a carry can't
live-test against it. `--host-net` is the route that unblocks it.

## What it does

A carry runs on a host that is itself on the tailnet, so `--network=host` makes
the container inherit that host's `tailscale0` and MagicDNS and reach the tower
directly - no in-container `tailscaled`, no auth key, no minting. The in-container
tailscale join (for a host *not* on the tailnet) is a documented follow-up the
issue sketches, not what ships here.

It is wired on the four container-spinning surfaces (`work`/`headless`/default,
`task`, `sandbox`/`explore`, `ask`) by a shared `hostNetFlag()`, threaded through
`upPlan.HostNet`, and emitted as `--network=host` from the shared
`dockerArgvHead`. The `--network=host` line shows up in `--print`.

## Off by default, and it implies `--aws`

`--host-net` **widens isolation**: the carry gets the host's full network view,
not the cwd-only default, so it is opt-in and only helps on a tailnet host.

Because the tower's FQDN is SSM-only by design (never hardcoded), a route with no
address resolver is useless - so `--host-net` **implies the `~/.aws` mount** (the
same read-only `/root/.aws` bind `--aws` adds). The two are always wanted
together for a tower carry. A tower carry is:

```bash
warded work coilyco-flight-deck/agent-proxy#1 --host-net
```

which resolves the FQDN from SSM and reaches `http://$TOWER:11434` inside the
container.

## See also

- [container.md](container.md) - the least-access model this widens.
- [agent-flags.md](agent-flags.md) - the launch flag list.
- [agent-credentials.md](agent-credentials.md) - how routes + creds are seeded.
