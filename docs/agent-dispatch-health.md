---
doc_goal: Define dispatch-health inputs, human and machine outputs, harness-native status rendering, and external alert-consumer boundary.
---
# Dispatch health

`ward agent dispatch-health` combines live issue-thread lifecycle signals and
the live engineer list. It does not maintain or refresh a backlog ledger.

* Redispatch markers provide queued and deferred counts.
* Reservations provide in-flight and stale-held counts.
* Trusted workflow records provide submitted, merge-ready, blocked, and failed counts.
* Container evidence provides running, partial-launch, launch-intent,
  cleanup-needed, and failed-before-start counts.
* Duplicate refs, backpressure, and runaway launch rates remain explicit signals.

Use the default human summary, `--line` for one stable
`WARD-DISPATCH-HEALTH:` line, or `--json` for the versioned snapshot.
`generated_at` is observation metadata, never orchestration state.

## Consumers

Ward injects the live status command only into a harness whose typed manifest
supports native status rendering. Claude currently does. Other harness config
is unchanged.

Interactive native desktop notification is a separate best-effort Ward output
when a local notifier and desktop are available. Hosted alerting, dashboards,
and other external consumers may parse the stable line or JSON, but their
routing, credentials, and delivery are outside Ward.

## See also

* [agent-ops.md](agent-ops.md) - live evidence surfaces.
* [agent-observability.md](agent-observability.md) - logs and schemas.
