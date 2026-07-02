# ward agent opencode

`opencode` is the local Ollama-backed harness behind the renamed `qwen` mode.

## Capabilities

- Host credential channel: none.
- Container config: writes `~/.config/opencode/opencode.json`.
- Install step: self-installs the standalone `opencode` binary at container start.

## Config shape

The config registers a local Ollama provider with the default model pinned to
`ollama/$WARD_QWEN_MODEL` at `$WARD_OLLAMA_URL`.

**Bring your own Ollama:** those two vars default to `qwen3-coder:30b` and
`http://localhost:11434/v1`, are read only at in-container bootstrap, and are **not**
threaded from the host - so setting them on your host does not repoint opencode
today. If you run your own Ollama, read [agent-local-model.md](agent-local-model.md)
first: on native Linux the host-net route makes the baked-in `localhost:11434` reach
your local Ollama, but Docker Desktop and repointing the endpoint/model are not
supported yet (#395).

## Install stance

Best-effort self-install. An image that already contains `opencode` short-circuits it.

## Launch dialect

- Host preflight: none.
- Headless: `opencode run <seed>`.
- Interactive: `opencode`.

## Smoke gate

No **host GO/NO-GO pre-flight**: opencode has no host one-shot wired, so dispatch
proceeds without a pre-dispatch check (see [agent-preflight.md](agent-preflight.md)).

There **is** an **in-container Ollama reachability probe** (pre-launch, ward#487),
the local-model analog of claude's auth smoke test. A headless opencode whose
Ollama endpoint is down would hang the dispatched container exactly like an
undetected bad claude credential (the failure mode a smoke gate exists to prevent).
So before launching, the entrypoint TCP-probes `WARD_OLLAMA_URL` (the endpoint the
opencode config binds) with a short retry window, and on an unreachable endpoint
**aborts the container with a clear error** naming the endpoint and how to recover,
instead of letting it silently hang. The probe is headless-only (an interactive TUI
has a human watching); set `WARD_SMOKE_TEST_SKIP=1` to bypass it (the same switch
claude's probe reads).

## See also

- [docs/agent-qwen.md](agent-qwen.md) - the deprecated `qwen` alias that resolves here.
- [docs/agent-local-model.md](agent-local-model.md) - bring your own Ollama: defaults, the supported route, and the current limitation (#395).
- [docs/agent-local-harnesses.md](agent-local-harnesses.md) - the local harness index.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
