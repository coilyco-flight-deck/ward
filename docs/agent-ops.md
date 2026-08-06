---
doc_goal: Define every supported read, status, stop, reap, and retained-dispatch operation in one operator reference.
---
# Agent operations

## Live and retained state

* `ward agent list [--json]` shows launch intents, running engineers,
  cleanup-needed records, execution budgets, capacity, and remaining slots.
* `ward agent dispatch list [--json]` lists retained broker requests.
* `ward agent dispatch status <request-id> [--json]` reads one public lifecycle.
* `ward agent director queue|status --repo owner/repo [--json]` reads tracker-backed work state.

The issue thread is reservation authority. Docker and `~/.ward` records are
operational evidence and cache. Failed-before-start and cleanup-needed records
remain visible but do not count as running capacity.

## Logs

`ward agent logs <target>` prefers live container output, then the drained
secret-safe archive. A target may be a run/container, issue ref, peer id, or
dispatch request. `--artifact` selects `console`, `transcript`, `meta`,
`friction`, `dispatch`, or the typed `release` sidecar. Ward never falls back
to a retired raw archive.

## Stop and reap

* `ward agent stop <target> [--print]` deliberately stops one engineer or peer,
  or clears a confirmed stale issue-ref launch after its confirmation window.
  It refuses fresh intents and ambiguous peer ids.
* `ward agent reap [--dry-run]` applies the idle-policy backstop to wedged
  engineers and stale intents. It does not target director, QA, or broker containers.
* `ward agent reservations clear` removes and recreates the disposable local
  reservation cache. It does not delete canonical issue-thread evidence.

## Retention

`ward agent dispatch prune` previews terminal request records older than 30
days by default. `--confirm` removes the selected journal and secret-safe
dispatch artifact. Active and cleanup-needed records are never auto-pruned.

None of these commands mutates the target checkout. Broker-connected read-only
surfaces forward supported operations through the supervised broker.

## See also

* [agent-dispatch-health.md](agent-dispatch-health.md) - health summary.
* [agent-reservation.md](agent-reservation.md) - stale launch recovery.
* [agent-observability.md](agent-observability.md) - artifact schemas.
* [agent-release.md](agent-release.md) - Director-to-Ops release contract.
* [agent-release-transaction.md](agent-release-transaction.md) - deploy-state transaction and recovery.
* [troubleshooting.md](troubleshooting.md) - symptom-to-remedy map.
