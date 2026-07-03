---
doc_goal: Explain why a startup role's host/cloud capability (the ~/.aws mount, the tailnet route) is per-role guardfile-set membership in the meld layer rather than a first-class ward flag, so an operator knows where capability is configured, how the advisor gets live-observe by default, and what the deprecated --aws/--tailnet aliases still do.
---
# ward agent: per-role capability ([ward#578](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/578))

Mounting `~/.aws` and joining the tailnet are **Kai-specific host config**. They
used to ride first-class `--aws` / `--tailnet` flags on every `ward agent` surface,
which meant an external `ward agent --help` showed vocabulary meaningless to anyone
but Kai - host/cloud config leaked **up** into the shipped product. Per the
config-placement law it belongs **down** in the meld layer.

## The model: capability is guardfile membership per role

A startup role's capability is now a **guardfile set** declared in
[`ward-kdl.fleet.kdl`](../cmd/ward-kdl/ward-kdl.fleet.kdl)'s `roles` block (a
dialect-2 fleet-config field, [ward-kdl.md](ward-kdl.md)). Each entry names a
guardfile, EITHER as a flat list or a `prefix="..."`:

- **advisor** holds the **live-observe set** - the aws + tailscale guardfiles - so
  advisor runs mount `~/.aws` and join the tailnet **with no flag**.
- **engineer** and **director** hold the **empty set** - the least-access wall.

The references are **descriptive guardfile names, never inline grants**: a
guardfile's own dialect-1 body declares the host mount / network it needs, and
membership just picks the set, so the dialect-2 roster stays permission-free (the
`mount`/`exec`/`can` tokens are still rejected there). See the dialect split in
[ward-kdl.md](ward-kdl.md).

## How ward resolves it

At launch ward reads the role's guardfile set and maps each **well-known capability
guardfile name** to the host mechanism it ships:

- `ward-kdl.aws.guardfile.kdl` -> mount `~/.aws` read-only.
- `ward-kdl.tailscale.guardfile.kdl` -> join the tailnet (host-net on native Linux,
  the SOCKS5 sidecar on Docker Desktop; [agent-host-net.md](agent-host-net.md)).

A tailnet grant still **implies the `~/.aws` mount** (the tower FQDN is SSM-held).
`advisor --no-tailnet` opts a rare isolated run **fully** back out (no tailnet, and
the role-granted `~/.aws` drops too).

## The mount only forwards host creds ([ward#579](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/579))

The `~/.aws` mount **forwards** the dispatch host's identity, it mints none, so the
capability only delivers SSM access when the host running `docker run` has creds
there. A credential-less host is the silent trap: docker mounts a **missing** source
as an **empty** dir, so `aws` / `ssm` calls fail `NoCredentials` deep in a script.

ward makes that gap **loud**: when the capability is on but `~/.aws` holds no
`config` nor `credentials` file, ward warns to stderr (mirroring `--host-net`). It
does **not** hard-fail. The fix is host-side: give each dispatch host an `~/.aws`
with SSM read/write so `--aws` self-serves a rotation.

## The deprecated `--aws` / `--tailnet` aliases

`--aws` and `--tailnet` survive one release as **hidden back-compat aliases** so
in-flight callers (the director's engineer forwarding, the dispatch broker) do not
break while they migrate to the role-set model. They **force** the capability on
regardless of role. New usage relies on the role's guardfile set, not these flags.
The hidden `--tailnet-mode auto|host-net|sidecar` still pins the mechanism.

## See also

- [agent-flags.md](agent-flags.md) - the launch flag surface.
- [ward-kdl.md](ward-kdl.md) - the dialect-2 fleet config the `roles` block lives in.
- [container.md](container.md) - the least-access model this keys into.
- [agent-advisor.md](agent-advisor.md) - the role that holds the live-observe set.

Cross-reference convention from [coilysiren/agentic-os#59](https://github.com/coilysiren/agentic-os/issues/59).
