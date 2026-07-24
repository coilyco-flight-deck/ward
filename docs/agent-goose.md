# warded goose

The `goose` role is the local Ollama-backed harness.

- It composes its Ollama endpoint into config.
- It accepts the shared model override API via `--config agent.goose.model=<model>`.
- It has no embedded model default. `WARD_GOOSE_MODEL` or the shared override
  must provide one before launch.
- It runs a host one-shot preflight before launch.
- It launches headless work with `goose run --no-session -t` so the process
  exits cleanly after the final turn. Ward feeds the prompt on stdin because
  Goose treats trailing values as CLI arguments.

## See also

- [agent-harnesses.md](agent-harnesses.md) - the harness comparison.
- [agent-preflight.md](agent-preflight.md) - the host preflight path.
- [agent-roster.md](agent-roster.md) - the generated roster entry.
