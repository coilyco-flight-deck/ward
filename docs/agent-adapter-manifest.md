# Agent-adapter manifest

The single source of **per-agent divergence** ward needs to drive a harness - the
binary it launches, how much context it carries, its argv dialect, its headless
stream format, and its auth. It exists so ward can become a *generic,
manifest-backed driver* instead of hardcoding each agent in three layers: ward-kdl
guardfiles, Go switches in
[`container_compute.go`](../cmd/ward/container_compute.go) (`agentBinary`,
`contextLevel`, `hostPreflightArgv`), and bash cases in
[`entrypoint.sh`](../cmd/ward/containerassets/entrypoint.sh).

This is the **aos pre-req for ward#152**. #152 removes those switches and cases; it
cannot be validated until the manifest it reads exists. This is that artifact, plus
a test that pins it to today's behavior.

## Where it lives, who publishes it

aos owns and publishes the canonical manifest. ward embeds a pinned copy at
[`agent-adapters.yaml`](../cmd/ward/containerassets/agent-adapters.yaml) so a
container needs no network to know its agent's dialect. The two are denormalized
copies - aos is upstream, the embedded copy tracks it - mirroring the substrate
manifest ([`preclone-repos.txt`](../cmd/ward/containerassets/preclone-repos.txt);
see [container-substrate.md](container-substrate.md)). The loader is
[`agent_adapter.go`](../cmd/ward/agent_adapter.go) (`loadAgentManifest` /
`parseAgentManifest`), modeled on `loadSubstrateManifest`.

## Schema (schemaVersion 1)

```yaml
schemaVersion: 1
agents:
  - name: claude            # the --mode value and short agent name
    binary: claude          # in-container command this agent launches
    contextLevel: 2         # least-access ladder: 2=full, 1=scoped, 0=minimal
    stream: stream-json     # headless stream format: stream-json | none
    auth: claude-keychain   # host-resolved credential: claude-keychain | none
    argv:
      preflight: [claude, -p]   # host one-shot prefix; prompt appended. []=none yet
      headless: [claude, -p, --verbose, --output-format, stream-json]  # seed appended
      interactive: [claude]     # seed appended unless it can't go on argv
```

Field notes:

- `contextLevel` drives the `WARD_CONTEXT_LEVEL` ladder the entrypoint composes
  context against (full/scoped/minimal); see [container.md](container.md).
- `stream: none` means the agent prints its own progress (goose), so ward pipes
  nothing through its stream-json filter.
- `argv.preflight: []` means no reliable host one-shot yet (codex/qwen), so the
  GO/NO-GO check bows out and dispatch proceeds unguarded (ward#147, ward#148).
- `argv.interactive` for goose is `[goose, session]`: no seed on argv, so the issue
  is pasted in by hand.
- `argv.headless` for codex is `[codex, exec]` - codex's non-interactive exec
  dialect, not claude's `-p` stream-json flags. codex prints its own progress, so
  its `stream` is `none` and ward pipes nothing through the stream-json filter
  (ward#178). Its auth is `codex-file`: the host's `~/.codex/auth.json`, injected
  into the container (see [agent.md](agent.md)).

`qwen` stays **provisional**: its binary is not installed in the dev-base image
yet, so its argv still mirrors the claude-style default branch in `entrypoint.sh`
and firms up when the binary lands. `codex` is now wired to its real exec dialect
(ward#178), though its install in the image remains the aos-side step.

## The contract test

[`agent_adapter_test.go`](../cmd/ward/agent_adapter_test.go) asserts the embedded
manifest agrees, entry for entry, with the still-live Go switches. That is what
makes this a real pre-req rather than parallel drift: when #152 swaps the switches
for manifest lookups, the swap is **provably behavior-preserving**. Change the
manifest and the switch in lockstep, or the test fails.

## See also

- ward#152 - the consumer: replace the switches/cases with manifest-backed lookup.
- [container.md](container.md) - the container the manifest drives.
- [agent.md](agent.md) - `ward agent <name>`, which selects the mode.
