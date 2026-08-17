# Dispatch health and recovery

What a restarted broker reconciles, and what the health verb reports.

## Dispatch health

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

## Dispatch recovery

Before reporting healthy, a restarted broker reconciles each nonterminal
journal in its Compose project against containers with the exact dispatch
request label.

* A pre-container request can replay because no worker started.
* A matching stopped `created` container is refreshed and started once.
* A running, restarting, or paused container is adopted without another start.
* An exited or dead container becomes failed.
* Start-begun with no matching container is ambiguous and becomes interrupted.
* Multiple matching containers are an invariant violation and block readiness.

This gives at-most-one container start per request id. It does not resume a
harness after its worker container exits.

The retained public record carries timestamps, repository, issue, role,
harness, workflow, last transition, terminal reason, and artifact path. It
never retains credentials, prompt or transcript bodies, or unbounded command
output. Recovery-only argv is discarded once a container becomes visible.

Use `ward agent dispatch status <request-id> --json` for the decision,
`ward agent logs <request-id>` for secret-safe evidence, and `ward agent
dispatch prune --confirm` only after the terminal retention period. Active and
cleanup-needed records cannot be pruned.

## See also

* [agent-dispatch-broker.md](agent-dispatch-broker.md) - request and authority contract.
* [agent-ops.md](agent-ops.md) - retained-state commands.
