---
doc_goal: Collapse the operational run surface into one page that covers the read-only director lane, logs, stop, list, and reap without scattering the operator contract across issue slices.
---
# ward agent ops

This page groups the on-demand operational surfaces around a run.

- `ward agent director` - the read-only supervisory lane.
- `ward agent director queue` / `status` - the read-only queue view for stale reservations, redispatch candidates, and open PR handoffs.
- `ward agent dispatch-health` - the dispatch pathology summary, status line feed, and alert line.
- `ward agent list` - show running engineers, launch intents, and capacity when the limit is known.
- `ward agent logs` - read one run's logs.
- `ward agent stop` - stop one visible running engineer on purpose. Ghost
  launch records are not stoppable here.
- `ward agent reap` - stop wedged engineer containers by idle policy and clear stale prelaunch reservations that never became visible.

## Shared contract

- These surfaces route through the broker when a brokered surface exists.
- The director view is read-only.
- Dispatch, reservation, reaper comments, and failure reporting use typed
  Forgejo/GitHub/GitLab/Shortcut adapters, not generated `ward ops` leaves.
- `list`, `logs`, `stop`, and `reap` all work against a specific run or
  container identity.
- `dispatch-health` and `list` treat the issue thread as the reservation source of
  truth. Docker and `~/.ward` are cache inputs, not authority.

## What to remember

- `logs` prefers the live container, then falls back to the drained archive.
- `logs` falls back to the harness-specific live transcript tree when `docker
  logs` is empty, then to the drained archive.
- `stop` and `reap` only target engineer containers, and `stop` refuses a
  reservation-only ghost record.
- A run that is already finished should not be treated as a new failure.

## Surface map

### list

`list` answers "what running engineers are active right now?" and shows the known limit plus remaining slots when available. It also surfaces launch intents before their container is visible and tags each entry with the current phase.

### logs

`logs` answers "what did this run last say?".

### stop

`stop` answers "stop this one run on purpose".

### reap

`reap` answers "this engineer is wedged and needs a host-side stop" and clears stale launch intents that never became visible.

## Operational notes

- `list` and `logs` are usually the first stop when a run seems stuck.
- `stop` is the manual correction path for a live engineer.
- Use `reap` or the stale-reservation cleanup path for a ghost launch record.
- `reap` is the safety net for idle engineer containers and stale launch intents.
- `reap` clears cache state, but the issue thread remains the canonical reservation record.
- none of these surfaces should surprise a caller with a write to the target
  repo.

## See also

- [agent-director.md](agent-director.md) - the director surface itself.
- [agent-dispatch-health.md](agent-dispatch-health.md) - the dispatch-health summary and alert line.
- [agent-lifecycle.md](agent-lifecycle.md) - how runs start.
- [troubleshooting.md](troubleshooting.md) - what to check when a run wedges.
