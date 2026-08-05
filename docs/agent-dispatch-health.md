---
doc_goal: Keep the dispatch-health surface and its alert line in one durable place so the status-line injection, operator HUD, and alert rules do not drift apart.
---
# ward agent dispatch-health

`ward agent dispatch-health` summarizes live issue-thread lifecycle signals and
the live engineer list. It does not read or refresh a local backlog ledger.

- Redispatch markers provide the queued and deferred counts.
- Fresh reservations provide the in-flight count.
- Stale prelaunch reservations provide the held count.
- Trusted workflow comments provide submitted, merge-ready, blocked, and failed counts.
- Container state provides running, partial-launch, launch-intent, cleanup-needed, and failed-before-start counts.
- Duplicate refs, stale prelaunch holds, backpressure, and runaway launch rates remain explicit signals.

## Surfaces

- `ward agent dispatch-health` - human summary.
- `ward agent dispatch-health --line` - the one-line status feed.
- `ward agent dispatch-health --json` - the machine-readable snapshot.

The JSON schema remains versioned. `generated_at` is observation metadata, not
orchestration state.

## Status line injection

Ward only injects the live status line into harnesses whose manifest says they
can render one. Claude is marked `StatusLine=true`. Other harnesses keep their
base settings unchanged.

## Alerts

Alert integrations match the stable `WARD-DISPATCH-HEALTH:` summary line.
Native desktop notification is best effort when an interactive desktop and
local notifier exist. Hosted alert routing remains outside Ward.

## See also

- [agent-director.md](agent-director.md) - attached supervision and queue JSON.
- [agent-harnesses.md](agent-harnesses.md) - which harness can render the status line.
- [agent-observability.md](agent-observability.md) - the log and envelope schema.
