---
doc_goal: Define broker restart reconciliation, at-most-one worker start, retained status, and explicit pruning recovery.
---
# Dispatch recovery

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
