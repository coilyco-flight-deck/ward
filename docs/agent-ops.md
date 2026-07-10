---
doc_goal: Collapse the operational run surface into one page that covers the read-only director lane, logs, stop, list, and reap without scattering the operator contract across issue slices.
---
# ward agent ops

This page groups the on-demand operational surfaces around a run.

- `ward agent director` - the read-only supervisory lane.
- `ward agent director queue` / `status` - the read-only queue view for stale reservations, redispatch candidates, and open PR handoffs.
- `ward agent list` - show live engineer runs.
- `ward agent logs` - read one run's logs.
- `ward agent stop` - stop one run on purpose.
- `ward agent reap` - stop wedged engineer containers by idle policy.

## Shared contract

- These surfaces route through the broker when a brokered surface exists.
- The director view is read-only.
- Dispatch, reservation, reaper comments, and failure reporting use typed
  Forgejo/GitHub/GitLab/Shortcut adapters, not generated `ward ops` leaves.
- `list`, `logs`, `stop`, and `reap` all work against a specific run or
  container identity.

## What to remember

- `logs` prefers the live container, then falls back to the drained archive.
- `logs` falls back to the live transcript tree when `docker logs` is empty,
  then to the drained archive.
- `stop` and `reap` only target engineer containers.
- A run that is already finished should not be treated as a new failure.

## Surface map

### list

`list` answers "what engineer runs are active right now?".

### logs

`logs` answers "what did this run last say?".

### stop

`stop` answers "stop this one run on purpose".

### reap

`reap` answers "this engineer is wedged and needs a host-side stop".

## Operational notes

- `list` and `logs` are usually the first stop when a run seems stuck.
- `stop` is the manual correction path.
- `reap` is the safety net for idle engineer containers.
- none of these surfaces should surprise a caller with a write to the target
  repo.

## See also

- [agent-director.md](agent-director.md) - the director surface itself.
- [agent-lifecycle.md](agent-lifecycle.md) - how runs start.
- [troubleshooting.md](troubleshooting.md) - what to check when a run wedges.
