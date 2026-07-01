# Agent-adapter manifest

The single source of **per-agent divergence** ward needs to drive a harness - the
binary it launches, how much context it carries, its argv dialect, its headless
stream format, and its auth. It lets ward be a *generic, manifest-backed driver*
instead of hardcoding each agent in Go switches
([`container_compute.go`](../cmd/ward/container_compute.go): `agentBinary`,
`contextLevel`, `hostPreflightArgv`) and bash cases
([`entrypoint.sh`](../cmd/ward/containerassets/entrypoint.sh)). ward#152 removes
those switches for manifest lookups; a test pins it to today's behavior first.

## Where it lives, who publishes it

The single hand-edited source is the dialect-2 fleet config
[`ward-kdl.fleet.kdl`](../cmd/ward-kdl/ward-kdl.fleet.kdl), embedded via
[`fleet.generated.kdl`](../cmd/ward/fleetassets/fleet.generated.kdl) (`make
sync-fleet-assets` mirrors it; a drift test fails the build), so a container needs
no network to know its agent's dialect. [`agent_adapter.go`](../cmd/ward/agent_adapter.go)
is the launcher-facing projection: `loadAgentManifest` parses the embedded fleet
(via [`fleet.go`](../cmd/ward/fleet.go)) and `fleetToAgentManifest` flattens it,
`validateAgentManifest` guarding the result. ward#419 (aos#310 §6) deleted the old
`agent-adapters.yaml` mirror + its `parseAgentManifest` loader; the KDL is now sole.

## Schema (schemaVersion 1)

The launcher reads this projected shape; the authoritative source is the KDL fleet
above. One agent's projection:

```yaml
name: claude            # the --driver value and short agent name
binary: claude          # in-container command this agent launches
contextLevel: 2         # least-access ladder: 2=full, 1=scoped, 0=minimal
stream: stream-json     # headless stream format: stream-json | none
auth: claude-keychain   # host-resolved credential: claude-keychain | codex-file | ollama | none
argv:
  preflight: [claude, -p]   # host one-shot prefix; prompt appended. []=none yet
  headless: [claude, -p, --verbose, --output-format, stream-json]  # seed appended
  interactive: [claude]     # seed appended unless it can't go on argv
```

Field notes:

- `contextLevel` drives the `WARD_CONTEXT_LEVEL` ladder the entrypoint composes
  context against (full/scoped/minimal); see [container.md](container.md).
- `stream: none` means the agent prints its own progress (goose/codex/opencode), so
  ward pipes nothing through its stream-json filter.
- `argv.preflight: []` means no host one-shot (codex/opencode), so the GO/NO-GO
  check bows out and dispatch proceeds (ward#147, ward#148).
- `argv.interactive` for goose is `[goose, session]`, opencode's `[opencode]`: no
  seed on argv, so the issue is pasted in by hand.
- `argv.headless` for codex is `[codex, exec]`, opencode's `[opencode, run]` - each
  its own dialect, not claude's stream-json flags (ward#178, ward#187).
- `auth: ollama` (goose, ward#186): goose binds the tower Ollama, whose endpoint
  ward resolves host-side from SSM and seeds into `~/.config/goose/config.yaml`.

The `opencode` entry (roster key renamed from `qwen` by ward#401; `--mode qwen`
still aliases) **self-installs at container start** (best-effort), so it needs no
image baking (ward#187).

## The contract test

[`agent_adapter_test.go`](../cmd/ward/agent_adapter_test.go) asserts the embedded
fleet agrees, entry for entry, with the still-live Go switches. `TestAgentManifest*`
pin the projection (binary, context level, argv dialect) to the switches;
`TestFleetSwitchesTwoWayPin` pins `fleet.generated.kdl` against the `parseMode`
roster. Once a three-way pin (fleet <-> YAML <-> switches, ward#415), it lost the
YAML leg in ward#419. Change the fleet and the switch in lockstep, or it fails.

## See also

- ward#152 - the consumer: replace the switches/cases with manifest-backed lookup.
- [container.md](container.md) - the container the manifest drives.
- [agent.md](agent.md) - `ward agent <surface> --driver <name>`, which selects the mode.
