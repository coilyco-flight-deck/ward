# ward agent: local harness index

The local harness pages are:

- [docs/agent-opencode.md](agent-opencode.md)
- [docs/agent-goose.md](agent-goose.md)

Both drive a local Ollama-backed model instead of a cloud API, so they need no host
credential channel - but they do depend on a **reachable Ollama endpoint**.

**Using your own Ollama?** Read [agent-local-model.md](agent-local-model.md) first:
it is the bring-your-own-Ollama page - what the defaults are, which route reaches a
local host Ollama today (native Linux, host-net), why Docker Desktop and endpoint
repointing are not supported yet, and the #395 anchor for the missing knob. The
Coily tower/tailnet routes below are the advanced path, not the only local-model
story.

## Failure path: Ollama unreachable (ward#487)

The sharpest failure mode for a local harness is an Ollama endpoint that is down or
unreachable: the host has no Ollama running, or the resolved tower host
(`WARD_GOOSE_OLLAMA_HOST_B64` / the SSM `/coilysiren/ollama/host` param) points
somewhere the container cannot dial. Left undetected, a headless run hangs the
dispatched container - the same silent-hang failure that claude's auth smoke test
([agent-credentials.md](agent-credentials.md)) exists to prevent.

So ward runs a **pre-launch Ollama-reachability probe**, the local-model analog of
that auth smoke test. Before a headless goose or opencode launches, the entrypoint
TCP-probes the endpoint the harness will dial, with a short retry window that
absorbs the `--ts-sidecar` loopback forwarder's startup:

- **goose** probes the `OLLAMA_HOST` seeded into `~/.config/goose/config.yaml`, or
  goose's built-in `http://localhost:11434` when no tower host resolved.
- **opencode** probes `WARD_OLLAMA_URL` (the endpoint its config binds).

On an unreachable endpoint the container **aborts with a clear error** naming the
endpoint and how to recover - point the harness at a live endpoint
(`WARD_OLLAMA_URL` for opencode, the SSM tower host for goose) or pass
`--ts-sidecar` to route `localhost:11434` to the tower - instead of hanging. The
probe is **headless-only** (an interactive session has a human watching) and
**bypassable** with `WARD_SMOKE_TEST_SKIP=1`, the same switch claude's probe reads.
It lives in both bootstrap paths: `smoke_test_ollama_reachable` in the entrypoint
and the `ollamaprobe` `LaunchGate` in the Go port. It is a **reachability** check,
not a model-serving check: a reachable-but-misconfigured Ollama (wrong model tag)
still surfaces at run time, not here.

## See also

- [docs/agent-local-model.md](agent-local-model.md) - bring your own Ollama: defaults, the supported native-Linux route, and the current limitation (#395).
- [docs/agent-credentials.md](agent-credentials.md) - the shared cloud credential channel.
- [docs/agent.md](agent.md) - the `ward agent` verb family and usage.
