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

Each completed log drain writes one run directory under
`~/.ward/agent-logs/<container>/`:

- `console.log`
- `transcript.jsonl` when the harness produced a transcript
- `meta.json`
- `skill-usage.json` when the transcript contains observed skill use

The parallel `~/.ward/agent-logs-redacted/<container>/` view contains
`console.redacted.log`, `transcript.redacted.jsonl` when redacted tool
envelopes are available, the same secret-free `meta.json`, and the same
`skill-usage.json` when present.

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
