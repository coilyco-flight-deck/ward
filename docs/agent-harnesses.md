---
doc_goal: Give the supported harness axis in one place so launch docs can point at a single comparison instead of a pile of per-harness pages.
---
# ward agent harnesses

`ward agent` can launch the same role through different harnesses.

- `claude` - the cloud subscription harness.
- `codex` - the OpenAI Codex harness.
- `goose` - the local model harness.
- `opencode` - the Ollama-backed local harness.

The harness choice affects credentials, preflight shape, and context level.

## Rule of thumb

- Cloud harnesses usually need host-side credentials and a preflight check.
- Local harnesses trade host-side auth for a local model endpoint check.
- `--harness` and `--agent` are equivalent spellings.

## Harness notes

### claude

`claude` is the default cloud path. It uses the host subscription login and
enters the container with the credential material ward seeds at launch.

### codex

`codex` is the OpenAI path. It uses the host-side auth file or login flow and
then launches the agent inside the container.

### goose

`goose` is the local-model path. It talks to an Ollama endpoint and does not
need the same cloud credential shape.

### opencode

`opencode` is the other local-model path. It shares the same Ollama-style
endpoint model and is useful when you want a lean local loop.

## Context level

The harness also picks the context level.

- cloud harnesses tend to carry more host doctrine.
- local harnesses tend to carry less host doctrine.
- the container env exports the chosen level for the entrypoint.

## Why this page exists

The old tree had one page per harness family plus separate local-model pages.
This page keeps the comparison in one place and lets the other docs stay short.

## See also

- [agent.md](agent.md) - the entrypoint.
- [agent-lifecycle.md](agent-lifecycle.md) - launch-time checks.
- [container-contract.md](container-contract.md) - the context and mount contract.
