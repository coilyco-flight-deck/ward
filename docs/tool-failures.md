# tool failures

Ward now owns the first live tool-failure producer for Claude runs.

- It drains the Claude transcript after a run finishes and looks for errored tool results.
- It writes schema-v1 JSONL rows to `~/.cache/agentic-os/tool-failures/<repo>.jsonl`.
- It keeps the record load-bearing fields stable for the shared shipper: `fingerprint`, `failure_class`, `harness`, `repo`, with scrubbed detail in `stderr_excerpt`.
- It is best-effort and never blocks the run.

## Scope

- Claude transcript capture only for now.
- Local buffer write only.
- No shipper move in ward.

## See also

- [agent-observability.md](agent-observability.md) - transcript and envelope plumbing.
- [FEATURES.md](FEATURES.md) - the shipped capability inventory.
