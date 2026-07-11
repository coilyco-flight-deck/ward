---
doc_goal: Keep the dispatch-health surface and its alert line in one durable place so the status-line injection, operator HUD, and alert rules do not drift apart.
---
# ward agent dispatch-health

`ward agent dispatch-health` summarizes the live dispatch surface.

- It reads the backlog ledger and the live engineer list.
- It reports queued, in-flight, held, submitted, merge-ready, deferred, and failed counts.
- It surfaces double-dispatch, backpressure, and runaway signals.
- It prints the stable `WARD-DISPATCH-HEALTH:` line that the director loop and alert rules can match.

## Surfaces

- `ward agent dispatch-health` - human summary.
- `ward agent dispatch-health --line` - the one-line status feed.
- `ward agent dispatch-health --json` - the machine-readable snapshot.

## Status line injection

Ward only injects the live status line into harnesses whose manifest says they can render one.

- Claude is marked `StatusLine=true`, so the bootstrap writes a `ward agent dispatch-health --line` status command into its settings.
- Other harnesses skip the injection and keep the base settings unchanged.

## Alerts

The same summary line is what the director heartbeat emits for alert matching.

- native desktop notification, when the operator has an interactive desktop and the local notifier exists.
- SigNoz alert rules on the existing ward agent-run envelope stream.
- Telegram and ntfy fallback channels for headless or remote runs.

## See also

- [agent-harnesses.md](agent-harnesses.md) - which harness can render the status line.
- [agent-ops.md](agent-ops.md) - the rest of the operator surfaces.
- [agent-observability.md](agent-observability.md) - the log and envelope schema.
