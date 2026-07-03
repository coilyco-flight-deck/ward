---
doc_goal: Let an operator repoint ward's tailnet network, proxy box, and ollama tower at their own infra through host-side WARD_* env overrides without a rebuild, and understand why these runtime-topology values carry overrides while deployment/spec-bundle data does not.
---
# Repointing the tailnet + tower topology

ward's tailnet routing ([agent-ts-sidecar.md](agent-ts-sidecar.md)) once baked its
network name, proxy box, and tower host/port as Go literals. Those are **infra data**,
not ward's own identity, so each now carries a `WARD_*` env override. The old literal
stays as the fail-safe default (env is the only source, so ward never removes it), and a
run that sets none behaves exactly as before.

## The overrides

Set these on the **host** that dispatches the run. ward resolves them host-side and hands
the result down, so the container needs no extra config.

- `WARD_TAILNET_NETWORK` - the shared docker network the run attaches to. Default `ward-tailnet`.
- `WARD_TAILNET_PROXY` - the standing SOCKS5 box's `host:port`. Default `mac-proxy:1055`. The host half doubles as the box's container name for the attach preflight.
- `WARD_TOWER_HOST` - the tower's MagicDNS node name. Default `kai-tower-3026`.
- `WARD_TOWER_OLLAMA_PORT` - the port the tower serves ollama on. Default `11434`.

## What flows down

The resolved `WARD_TS_SOCKS5`, `WARD_TOWER_OLLAMA`, and `WARD_TOWER_OLLAMA_LOCAL` still
ride into the run as before. An override also propagates `WARD_TOWER_HOST` and
`WARD_TOWER_OLLAMA_PORT`, so the in-container `ward container forward` bridge dials the
same tower the host resolved.

## Scope note

This is the runtime-topology half of the [agentic-os#308](https://github.com/coilysiren/agentic-os/issues/308) Bucket C audit. The forge base,
registry image, and SSM paths are deployment/spec-bundle data and move with [ward#453](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/453) /
[ward#503](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/503), not here. Per-agent model + provider values are Bucket A.

## See also

- [agent-ts-sidecar.md](agent-ts-sidecar.md) - the tailnet route these values configure.
