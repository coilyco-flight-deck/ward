---
doc_goal: Explain why a startup role's host/cloud capability (AWS creds, the tailnet route) is per-role guardfile-set membership in the meld layer rather than a first-class ward flag, so an operator knows where capability is configured, how the advisor gets live-observe by default, and what the deprecated --aws/--tailnet aliases still do. The aws creds mechanism itself is in agent-aws-creds.md.
---
# ward agent: per-role capability ([ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578))

AWS creds and the tailnet are **Kai-specific host config**. They used to ride
first-class `--aws` / `--tailnet` flags on every `ward agent` surface, leaking
host/cloud config **up** into the shipped product (an external `ward agent --help`
showed vocabulary meaningless to anyone but Kai). Per the config-placement law it
belongs **down** in the meld layer.

## The model: capability is guardfile membership per role

A startup role's capability is now a **guardfile set** declared in
[`ward-kdl.fleet.kdl`](../cmd/ward-kdl/ward-kdl.fleet.kdl)'s `roles` block (a
dialect-2 field, [ward-kdl.md](ward-kdl.md)). Each entry names a guardfile, as a
flat list or a `prefix="..."`:

- **advisor** and **director** hold the **live-observe set** - the aws + tailscale
  guardfiles - so their runs get AWS creds and join the tailnet **with no flag**
  (the director to observe kai-server, [eco-observe.md](eco-observe.md)).
- **engineer** holds the **empty set**, the least-access wall.

The references are **descriptive guardfile names, never inline grants**: a
guardfile's dialect-1 body declares the mount / network it needs, and membership
just picks the set, so the dialect-2 roster stays permission-free. See the dialect
split in [ward-kdl.md](ward-kdl.md).

## How ward resolves it

At launch ward maps each **well-known capability guardfile name** in the role's set
to the host mechanism it ships:

- `ward-kdl.aws.guardfile.kdl` -> **export the host's AWS credential chain and inject
  it as `AWS_*` env**, mount `~/.aws` the fallback ([agent-aws-creds.md](agent-aws-creds.md)).
- `ward-kdl.tailscale.guardfile.kdl` -> join the tailnet ([agent-host-net.md](agent-host-net.md)).

A tailnet grant still **implies the aws capability** (the tower FQDN is SSM-held).
`advisor --no-tailnet` opts a rare isolated run **fully** back out (no tailnet, and
the aws capability drops too).

## The aws capability delivers creds by export-and-inject ([ward#586](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/586))

When the aws capability is on, ward exports the **launching host's** whole AWS credential
chain (`aws configure export-credentials` - SSO, env, profile, assumed role, IMDS) and
injects it as short-lived `AWS_*` env, so the run gets working creds regardless of host
auth source and with **zero in-container chain replication**. A `~/.aws` read-only mount is
the fallback when export fails. The full mechanism (region injection, the broker-forwarded
and Windows paths, the [ward#579](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/579)
warning rule) is in [agent-aws-creds.md](agent-aws-creds.md).

## The deprecated `--aws` / `--tailnet` aliases

`--aws` and `--tailnet` survive one release as **hidden back-compat aliases** so
in-flight callers (the director's engineer forwarding, the dispatch broker) do not
break while they migrate to the role-set model. They **force** the capability on
regardless of role. The hidden `--tailnet-mode auto|host-net|sidecar` still pins the
mechanism.

## See also

- [agent-aws-creds.md](agent-aws-creds.md) - how the aws capability delivers creds.
- [agent-flags.md](agent-flags.md) - the launch flag surface.
- [ward-kdl.md](ward-kdl.md) - the dialect-2 fleet config the `roles` block lives in.
- [agent-advisor.md](agent-advisor.md) - the role that holds the live-observe set.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
