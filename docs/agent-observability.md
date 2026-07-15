---
doc_goal: Keep the observability anchor stable after the log-schema pages were collapsed.
---
# agent observability

This page is the durable anchor for the agent log and envelope schema.

- It covers drained console views, redacted transcripts, and tool envelopes.
- It keeps body-shaped payloads out of the persisted envelope stream.
- It carries optional per-run `meta.json` friction events for missing
  transcripts, prelaunch failures, salvage preservation, and extra-repo
  preservation so later review does not have to scrape console logs.
- It also names the stable `WARD-DISPATCH-HEALTH:` line that alert rules can match in the existing agent-run stream.
- It is the home for the redaction and cardinality discipline comments.

## See also

- [agent-ops.md](agent-ops.md) - logs, stop, list, reap.
- [troubleshooting.md](troubleshooting.md) - the symptom-indexed entry point.
