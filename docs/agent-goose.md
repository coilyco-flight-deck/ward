# ward agent goose

`goose` is the other local harness, with provider config seeded by ward.

## Capabilities

- Host credential channel: none.
- Container config: writes `~/.config/goose/config.yaml`.
- Config seed includes `GOOSE_PROVIDER`, `GOOSE_MODEL`, and `OLLAMA_HOST`.

## Config shape

Ward resolves the host Ollama endpoint and seeds it base64'd as
`WARD_GOOSE_OLLAMA_HOST_B64`, then composes the goose config from that plus the
provider and model overrides.

**Bring your own Ollama:** the endpoint here is resolved **host-side from SSM** (the
Coily tower) or falls back to goose's built-in `http://localhost:11434`, and it is
**not** user-overridable from the host today. If you run your own Ollama, read
[agent-local-model.md](agent-local-model.md) before launching: on native Linux the
host-net route shares your local Ollama, but Docker Desktop and repointing the
endpoint are not supported yet (#395).

## Install stance

goose is image-baked from the launcher point of view. No self-install step.

## Launch dialect

- Host preflight: the detached GO/NO-GO gate, at parity with claude
  ([agent-preflight.md](agent-preflight.md)). goose answers it via `goose run -t`
  when the dispatch itself is interactive (a human at the TTY), even though the
  run it gates is detached. It is skipped for a scripted/piped dispatch, on
  `--print`, and with `--no-preflight` - the same skip rules claude follows, not
  a goose carve-out.
- Headless: `goose run -t <seed>`.
- Interactive: `goose session` with the issue pasted in by hand.

## Smoke gate

Two gates sit at different points, and only one applies here:

- **Host GO/NO-GO pre-flight** (pre-dispatch, [agent-preflight.md](agent-preflight.md)): goose keeps parity with claude - it answers via `goose run -t` before a detached run launches.
- **In-container Ollama reachability probe** (pre-launch, ward#487): the local-model analog of claude's auth smoke test. A headless goose whose Ollama endpoint is down would hang the dispatched container exactly like an undetected bad claude credential (the failure mode a smoke gate exists to prevent). So before launching, the entrypoint TCP-probes the endpoint goose will dial - the `OLLAMA_HOST` seeded into `config.yaml`, or goose's built-in `http://localhost:11434` when no tower host resolved - with a short retry window (absorbing the `--ts-sidecar` forwarder's startup). On an unreachable endpoint it **aborts the container with a clear error** naming the endpoint and how to recover (a live tower host, or `--ts-sidecar`), instead of letting it silently hang. The probe is headless-only (an interactive session has a human watching); set `WARD_SMOKE_TEST_SKIP=1` to bypass it (the same switch claude's probe reads).

## See also

- [docs/agent-local-model.md](agent-local-model.md) - bring your own Ollama: defaults, the supported route, and the current limitation (#395).
- [docs/agent-local-harnesses.md](agent-local-harnesses.md) - the local harness index.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
