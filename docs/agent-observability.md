---
doc_goal: Keep the observability anchor stable after the log-schema pages were collapsed.
---
# agent observability

This page is the durable anchor for the agent log and envelope schema.

- It covers drained console views, redacted transcripts, and tool envelopes.
- It keeps body-shaped payloads out of the persisted envelope stream.
- It carries optional per-run `meta.json` friction events plus a versioned run
  summary block with raw and normalized outcomes, workflow, timing, artifact
  paths, git refs, and outcome signals so later review does not have to scrape
  console logs.
- The summary block is written atomically with the final `meta.json`, and the
  console drain gets a convenience footer of the form `WARD-RUN-SUMMARY:
  outcome=<normalized_outcome> meta=meta.json transcript=<path>`.
- It also names the stable `WARD-DISPATCH-HEALTH:` line that alert rules can match in the existing agent-run stream.
- It is the home for the redaction and cardinality discipline comments.

## Drain artifact contract

Each completed log drain writes one secret-safe run directory under
`~/.ward/agent-logs-redacted/<container>/`:

- `console.log`, sanitized before the atomic write
- `transcript.jsonl` when the harness produced safe tool envelopes
- `meta.json`
- `skill-usage.json` when the transcript contains observed skill use

There is no parallel raw archive or privileged raw-log escape hatch. The
redactor combines built-in secret shapes, exact nonempty values for credentials
Ward injected, and operator-local `agent.redaction` rules. It runs before Ward
persists or renders console output, stdout, stderr, broker logs, metadata error
fields, failure summaries, and structured tool envelopes. Body-shaped tool
arguments and tool-result bodies are dropped instead of retained.

After a safe drain, Ward overwrites harness JSONL transcripts inside a retained
exited container. A sanitization or overwrite failure leaves the container for
explicit recovery and does not mark the run drained.

The residual boundary is the running process: an agent and its harness-owned
transient session files necessarily see credential material before safe drain.
Ward guarantees only artifacts it persists and logs it renders.

Ward does not automatically delete or retroactively sanitize historical raw
archives under `~/.ward/agent-logs/`. `ward doctor` warns with that exact path
when such archives remain.

`skill-usage.json` schema version 1 carries only stable run dimensions:
`run_id`, `container`, `role`, `harness`, `repo`, `issue_ref`, `workflow`, and
`ward_version`. Its sorted `skills` rows carry `skill_name`, `count`,
`first_seen`, and `last_seen`. Ward recognizes Claude `Skill` tool calls and
Codex execution tool calls that read a selected `skills/.../SKILL.md` file.
Repeated references to one skill in a single Codex tool call count once.

The artifact never copies prompts, skill arguments or bodies, commands, tool
results, or transcript payloads. It is absent when no skill use is observed;
the drain removes a stale copy if a deterministic run directory is reused.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [troubleshooting.md](troubleshooting.md) - the symptom-indexed entry point.
