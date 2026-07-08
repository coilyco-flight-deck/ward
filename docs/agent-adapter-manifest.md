---
doc_goal: Show why the fleet manifest is the single source of per-agent divergence that lets ward be a generic manifest-backed harness driver rather than a pile of hardcoded Go switches, and give a spec author the projected schema to edit against.
---
# Agent-adapter manifest

The single source of **per-agent divergence** ward needs to drive a harness is
the effective fleet manifest (`fleetconfig.Fleet`) and its launcher projection.
Ward resolves ward-owned frontier defaults first, then lets a sparse bundle
override them. It covers the binary it launches, how much context it carries,
its argv dialect, its headless stream format, and its auth. It lets ward be a
*generic, manifest-backed driver* instead of hardcoding each agent in Go switches
([`container_compute.go`](../cmd/ward/container_compute.go): `agentBinary`,
`contextLevel`, `hostPreflightArgv`) and bash cases
([`entrypoint.sh`](../cmd/ward/containerassets/entrypoint.sh)). [ward#152](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/152) removes
those switches for manifest lookups; a test pins it to today's behavior first.

## Where it lives, who publishes it

The hand-edited source is the dialect-2 fleet config
[`ward-kdl.fleet.kdl`](../.ward/ward-kdl/ward-kdl.fleet.kdl), embedded via
[`fleet.generated.kdl`](../cmd/ward/fleetassets/fleet.generated.kdl) (`make
sync-fleet-assets` mirrors it; a drift test fails the build), so a container
needs no network to know its agent's dialect. [`agent_adapter.go`](../cmd/ward/agent_adapter.go)
is the launcher-facing projection: `loadAgentManifest` parses the effective
fleet (via [`fleet.go`](../cmd/ward/fleet.go)) and `fleetToAgentManifest`
flattens it, `validateAgentManifest` guarding the result. The docs page is the
schema note for that projection.

## Schema (schemaVersion 1)

The launcher reads this projected shape; the authoritative source is the KDL fleet
above. One agent's projection:

```yaml
name: claude            # the --harness value and short agent name
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
  check bows out and dispatch proceeds.
- `argv.interactive` for goose is `[goose, session]`, opencode's `[opencode]`: no
  seed on argv, so the issue is pasted in by hand.
- `argv.headless` for codex is `[codex, exec]`, opencode's `[opencode, run]` - each
  its own dialect, not claude's stream-json flags.
- `auth: ollama` (goose): goose binds the tower Ollama, whose endpoint
  ward resolves host-side from SSM and seeds into `~/.config/goose/config.yaml`.

The `opencode` entry (roster key renamed from `qwen`, `--mode qwen`
still aliases) **self-installs at container start** (best-effort), so it needs no
image baking. A sparse top-level bundle may omit `agent codex { ... }` or `agent
goose { ... }` and still get the ward built-ins back during resolution.

## The contract test

[`agent_adapter_test.go`](../cmd/ward/agent_adapter_test.go) asserts the effective
fleet agrees, entry for entry, with the still-live Go switches. `TestAgentManifest*`
pin the projection (binary, context level, argv dialect) to the switches;
`TestFleetSwitchesTwoWayPin` pins `fleet.generated.kdl` against the `parseMode`
roster. Formerly a three-way pin, it lost the YAML leg. Change the fleet and the
switch in lockstep, or it fails.

## See also

- [ward#152](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/152) - the consumer: replace the switches/cases with manifest-backed lookup.
- [container.md](container.md) - the container the manifest drives.
- [agent.md](agent.md) - `ward agent <surface> --harness <name>`, which selects the mode.
