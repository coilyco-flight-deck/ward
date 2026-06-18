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
    auth: claude-keychain   # host-resolved credential: claude-keychain | codex-file | ollama | none
    argv:
      preflight: [claude, -p]   # host one-shot prefix; prompt appended. []=none yet
      headless: [claude, -p, --verbose, --output-format, stream-json]  # seed appended
      interactive: [claude]     # seed appended unless it can't go on argv
```

Field notes:

- `contextLevel` drives the `WARD_CONTEXT_LEVEL` ladder the entrypoint composes
  context against (full/scoped/minimal); see [container.md](container.md).
- `stream: none` means the agent prints its own progress (goose/codex/qwen), so
  ward pipes nothing through its stream-json filter.
- `argv.preflight: []` means no host one-shot (codex/qwen), so the GO/NO-GO check
  bows out and dispatch proceeds (ward#147, ward#148); for qwen this is structural,
  as ollama runs in-container.
- `argv.interactive` for goose is `[goose, session]` and qwen's is `[opencode]`:
  no seed on argv, so the issue is pasted in by hand.
- `argv.headless` for codex is `[codex, exec]` and qwen's is `[opencode, run]` -
  each its own dialect, not claude's stream-json flags (ward#178, ward#187). codex
  auth is `codex-file` (host `~/.codex/auth.json`); qwen auth `none`.
- `auth: ollama` (goose, ward#186): goose binds the tower Ollama, whose endpoint
  ward resolves host-side from SSM and seeds into `~/.config/goose/config.yaml`.

`qwen` no longer mirrors the claude default branch (ward#187): the entrypoint writes
its opencode config and **self-installs opencode at container start** (best-effort),
so qwen is available without the image baking it in. See [agent.md](agent.md).

## The contract test

[`agent_adapter_test.go`](../cmd/ward/agent_adapter_test.go) asserts the embedded
manifest agrees, entry for entry, with the still-live Go switches. When #152 swaps
the switches for manifest lookups, the swap is **provably behavior-preserving**.
Change the manifest and the switch in lockstep, or the test fails.

## See also

- ward#152 - the consumer: replace the switches/cases with manifest-backed lookup.
- [container.md](container.md) - the container the manifest drives.
- [agent.md](agent.md) - `ward agent <name>`, which selects the mode.
