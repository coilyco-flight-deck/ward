# ward agent opencode

`opencode` is the local Ollama-backed harness behind the renamed `qwen` mode.

## Capabilities

- Host credential channel: none.
- Container config: writes `~/.config/opencode/opencode.json`.
- Install step: self-installs the standalone `opencode` binary at container start.

## Config shape

The config registers a local Ollama provider with the default model pinned to
`ollama/$WARD_QWEN_MODEL` at `$WARD_OLLAMA_URL`.

## Install stance

Best-effort self-install. An image that already contains `opencode` short-circuits it.

## Launch dialect

- Host preflight: none.
- Headless: `opencode run <seed>`.
- Interactive: `opencode`.

## Smoke gate

None. The model is local to the container, so dispatch proceeds without a host
GO/NO-GO check.

## See also

- [docs/agent-local-harnesses.md](agent-local-harnesses.md) - the local harness index.
- [docs/agent.md](agent.md) - the roster and roles vs harnesses split.
