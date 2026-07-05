---
doc_goal: Make a reader operate `ward ops eco observe` - the read-only window the warded director gets on the live kai-server - knowing what each verb reads, why it is safe (sealed verbs + a traversal-guarded cat), and what host prereqs the tailnet-routed ssh hop needs.
---
# ward ops eco observe

`ward ops eco observe` is the **read-only** third subtree of the Eco pipeline
([eco-test.md](eco-test.md)), split from the `server` promote surface so a
non-mutating observer never touches the write path ([ward#547](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/547)).
It runs over the same declared `ssh kai@kai-server` hop as the `server` verbs, and
exists for the **warded director**: the director now holds the live-observe
capability set (aws + tailnet, [agent-capability.md](agent-capability.md)), so its
container joins the tailnet and can reach kai-server directly.

## Verbs

```bash
ward ops eco observe status        # systemctl status eco-server.service --no-pager
ward ops eco observe logs          # journalctl -u eco-server.service -n 500 --no-pager
ward ops eco observe mods          # ls -la .../EcoServer/Mods   (audit the mod set)
ward ops eco observe configs       # ls -la .../EcoServer/Configs (confirm a config landed)
ward ops eco observe read-config <abs-path>   # cat one Configs file, e.g. EcoTelemetry.json
```

The four inspection verbs are `sealed`: they forward their pinned command exactly,
accepting no trailing caller arguments, so a read can never be widened into an
arbitrary remote command. `read-config` is the one verb that takes a caller path;
its `deny-when arg0` guard denies path traversal (`..`) and the obvious on-host
secret paths (`.ssh/`, `id_rsa`, `id_ed25519`, `.aws/`, `*credential*`,
`*secret*`), and `cat` is non-mutating regardless.

## Read-only only

No `restart`, `apply`, or `snapshot` here: those stay on the `server` promote
pipeline, which the director must **dispatch** (a headless engineer run), never run
itself. The observe surface is the safe second-set-of-eyes; every mutation stays
behind the write pipeline's guarded envelope.

## Concrete uses (the eco-ops tickets it unblocks)

- audit the live mod set + DLL versions - `mods` ([coilyco-gaming/eco-ops#15](https://github.com/coilyco-gaming/eco-ops/issues/15)).
- confirm a config landed - `configs` + `read-config .../Configs/*.eco` ([coilyco-gaming/eco-ops#21](https://github.com/coilyco-gaming/eco-ops/issues/21)).
- confirm EcoTelemetry is flowing - `read-config .../Configs/EcoTelemetry.json` + `logs` ([coilyco-gaming/eco-ops#26](https://github.com/coilyco-gaming/eco-ops/issues/26)).

## Host prereqs

The tailnet route (the director's default live-observe set) plus ssh access to
`kai@kai-server` are host config, not carried by ward. Reading a **system** unit's
journal needs `kai` in the `systemd-journal` (or `adm`) group - the same prereq the
`server health` probe documents in [ops-eco.md](ops-eco.md).

## See also

- [ward-kdl/ward-kdl.eco-observe.guardfile.md](ward-kdl/ward-kdl.eco-observe.guardfile.md) - the generated verb reference.
- [eco-test.md](eco-test.md) - the `native`/`server` promote siblings.
- [agent-capability.md](agent-capability.md) - the director's live-observe capability set.
