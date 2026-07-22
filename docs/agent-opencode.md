# warded opencode

The `opencode` role is the local Ollama-backed harness.

- It uses the local model endpoint instead of host credentials.
- It has no host one-shot preflight.
- It launches headless work with `opencode run`.
- Its pre-launch model check uses the configured OpenAI-compatible `/v1/models`
  endpoint. Native Ollama `/api/tags` probing remains separate.
- It writes `x-request-id` and `x-ward-*` correlation headers so agent-proxy
  can join traces back to the ward run.

## See also

- [agent-harnesses.md](agent-harnesses.md) - the harness comparison.
- [container-contract.md](container-contract.md) - the local runtime contract.
- [agent-roster.md](agent-roster.md) - the generated roster entry.
