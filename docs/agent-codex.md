# warded codex

The `codex` role is the OpenAI harness.

- It reads host auth from `~/.codex`.
- It has no host one-shot preflight.
- It launches headless work with `codex exec -- <prompt>`, so a dash-prefixed
  seed remains prompt text instead of becoming a CLI option.

## See also

- [agent-harnesses.md](agent-harnesses.md) - the harness comparison.
- [agentsapi.md](agentsapi.md) - the shared agent contract.
- [agent-roster.md](agent-roster.md) - the generated roster entry.
