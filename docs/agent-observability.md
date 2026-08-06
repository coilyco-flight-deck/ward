---
doc_goal: Define every secret-safe run artifact, summary and envelope schema, redaction guarantee, and residual exposure boundary.
---
# Agent observability

Each completed drain writes one secret-safe directory under
`~/.ward/agent-logs-redacted/<container>/`:

* `console.log` - sanitized console output.
* `transcript.jsonl` - safe tool envelopes when the harness produced them.
* `meta.json` - versioned run summary and optional friction events.
* `skill-usage.json` - present only when supported skill use was observed.

Broker wrapper artifacts live under the `dispatch/` subtree and follow the
same redaction path. Ward has no raw-artifact fallback.

## Redaction and envelopes

Before persistence or rendering, Ward applies built-in credential patterns,
exact nonempty credential values it injected, and operator
`agent.redaction` rules. Body-shaped tool arguments and results are dropped,
not scrubbed in place. Error fields, broker logs, summaries, console output,
and tool envelopes all pass through the same secret-safe path.

After a safe drain, Ward overwrites harness JSONL in a retained exited
container. A drain or overwrite failure leaves cleanup-needed state for
explicit recovery and does not claim a successful drain.

## Summary schema

`meta.json` records stable correlation fields, raw and normalized outcomes,
workflow, timing, artifact paths, Git refs, outcome signals, and a versioned
friction `events` array. The console receives a convenience
`WARD-RUN-SUMMARY:` footer that points to metadata and transcript artifacts.

`skill-usage.json` schema 1 contains run id, container, role, harness,
repository, issue ref, workflow, Ward version, and sorted skill rows with name,
count, first seen, and last seen. It never copies prompts, arguments, command
bodies, results, or transcript payloads.

## Residual boundary

The running harness and its transient session files necessarily see injected
credentials before drain. Ward guarantees artifacts it persists and logs it
renders. It does not retroactively migrate or sanitize retired raw archives at
`~/.ward/agent-logs/`. `ward doctor` warns when they remain.

## See also

* [agent-ops.md](agent-ops.md) - artifact selectors.
* [troubleshooting.md](troubleshooting.md) - evidence and remedies.
