---
doc_goal: Map common Ward symptoms to the smallest supporting evidence and a safe product-owned remedy.
---
# Troubleshooting

## Launch preview or run is refused

* Symptom - no container appears and the command names trust, target,
  reservation, capacity, config, auth, or Docker.
* Evidence - the command error, issue-thread reservation, `ward doctor`, and
  `ward agent list --json`.
* Remedy - correct the named input. For a stale prelaunch record, preview then
  run `ward agent stop owner/repo#N`. Do not override a visible live engineer.

## Run is silent or appears stuck

* Symptom - a container or dispatch request exists without useful terminal output.
* Evidence - `ward agent dispatch status <request-id> --json`, `ward agent
  logs <request-id>`, then `ward agent list --json`.
* Remedy - wait while state is active. Stop one confirmed wedged run with
  `ward agent stop`. Use `ward agent reap` for the idle-policy backstop.

## Capacity includes a ghost launch

* Symptom - list shows `container starting`, `cleanup-needed`, or a launch
  intent without `started_at`.
* Evidence - list JSON plus the canonical issue thread.
* Remedy - use `ward agent stop owner/repo#N --print`, then the real stop. If
  only local cache is stale, run `ward agent reservations clear` and recheck.

## Work did not land

* Symptom - the harness exited, but `main`, the remote branch, or the pull
  request lacks the expected candidate.
* Evidence - issue workflow comment, remote Git refs, PR status, run summary,
  and any retained rescue manifest.
* Remedy - follow the selected workflow. Use `ward agent pr recover` for a
  closed-unmerged PR or `ward agent recover` for a verified rescue artifact.

## Logs or artifacts are missing

* Symptom - live Docker logs are empty or one completed artifact is unavailable.
* Evidence - `ward agent logs <target> --artifact console|transcript|meta|friction|dispatch`.
* Remedy - use the returned secret-safe summary. Ward never falls back to a
  raw archive. `ward doctor` only warns when retired raw archives remain.

## `ward exec` refuses a repository command

* Symptom - unknown verb, uncommitted `.ward/ward.yaml`, or denied argv. A
  detached HEAD, a missing upstream, and an out-of-sync branch no longer refuse.
* Evidence - `.ward/ward.yaml`, `ward git status`, and `ward audit tail`.
* Remedy - declare the verb, commit the outstanding `ward.yaml` change, or fix
  the denied command shape. Use the dirty override only for a deliberate emergency.

## Container exits under memory or privilege pressure

* Symptom - Docker reports `OOMKilled=true`, or jailed `sudo`/SSH ownership checks fail.
* Evidence - container state and secret-safe console artifact.
* Remedy - correct host memory pressure. Move privileged convergence outside
  the jailed command rather than weakening the repository gate.

## See also

* [agent-ops.md](agent-ops.md) - operational commands.
* [agent-observability.md](agent-observability.md) - artifact contract.
* [agent-reservation.md](agent-reservation.md) - canonical state and cache cleanup.
