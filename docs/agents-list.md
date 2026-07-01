# `ward agents list` - the fleet roster read surface

`ward agents list` dumps ward's embedded fleet roster - the agent names and their
launch manifest - straight from `fleetconfig.Fleet`, the same parse
`cmd/ward/fleet.go` embeds (`fleetassets/fleet.generated.kdl`). The roster the
binary launches and the roster it reports are one source, so they cannot drift
(aos#310 §6, ward#417).

## Why it exists

aos's `scripts/agent-compat.py` checks its own agent-adapter list against ward's
roster. Without a stable surface it would re-parse ward's KDL or shadow the
roster - a drift vector, a fourth copy that silently goes stale. `--json` is the
one blessed read surface, so aos consumes ward's own parse instead of guessing at
it (this unblocks the aos agent-compat repoint, aos#310 issue 5).

## Forms

- `ward agents list` - a human table: the dialect version, the default agent, and
  one block per agent (binary, context-level floor, model).
- `ward agents list --json` - the stable machine surface.

Both read via `fleetconfig.Fleet` only. The command is deliberately independent
of the `agent_adapter` container-bootstrap surface, so it neither reads nor
touches `container_bootstrap.go` / `agent_adapter.go`.

## The `--json` schema

A deterministic object - every key is always present, so a consumer sees one
shape regardless of which optional fields an agent declares:

```json
{
  "schema_version": 2,
  "defaults": {
    "agent": "claude",
    "attribution": { "name": "...", "email": "..." }
  },
  "agents": [
    {
      "name": "codex",
      "binary": "codex",
      "context_level": 1,
      "stream": "none",
      "auth": "codex-file",
      "model": "gpt-5.4-mini",
      "endpoint": "",
      "provider": "",
      "reasoning_effort": "low",
      "verbosity": "low",
      "argv": {
        "preflight": null,
        "headless": ["codex", "exec"],
        "interactive": ["codex"]
      }
    }
  ]
}
```

Field notes:

- `context_level` carries the `-1` unset sentinel verbatim, so a consumer can
  tell "floor 0" (minimal context) from "no floor declared".
- Unset string knobs are `""`, not omitted - the key is always present.
- Each `argv` mode is the full token list (binary first), or `null` when the
  agent does not declare that mode. `agents` is in fleet-KDL source order.

## Where it mounts

`list` is a **hand-written leaf** under the `agents` group. The exec-dialect
ward-kdl guardfile launchers (`ward agents claude`, `ward agents codex`, ...)
auto-mount into that same group; hand-written surfaces win path collisions, so
`list` sits beside the launchers and the mount never clobbers it. See
[ward-kdl-in-ward.md](ward-kdl-in-ward.md).

## See also

- [ward-kdl-in-ward.md](ward-kdl-in-ward.md) - how the `agents` launchers auto-mount.
- [FEATURES.md](FEATURES.md) - the feature inventory.
