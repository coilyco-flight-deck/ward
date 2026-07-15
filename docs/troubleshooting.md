---
doc_goal: Give a short symptom-indexed entry point for failed runs so readers do not have to guess which subsystem to read next.
---
# troubleshooting

Start here when a run failed or seemed to do nothing.

## Common cases

- launched then nothing happened.
- never launched.
- `ward exec` refused.
- the run never landed on `main`.
- capacity looks full even though `ward agent list` shows a ghost `container
  starting` or `cleanup-needed` record.

## What to check

- `~/.ward/agent-logs/<container>/`.
- the reservation comment on the issue.
- the preflight or trust gate that blocked the launch.
- if the issue is already terminal, stale reservation cleanup is not the fix.

## Fast triage

1. decide whether the run launched.
2. decide whether the run landed.
3. decide whether the problem is in launch, workflow, or teardown.
4. use the narrowest page that matches the failure.

## Symptom hints

- if the launch never started, the issue is usually in preflight or trust.
- if the container started but nothing happened, the issue is usually in
  credentials or harness startup.
- if the branch exists but the merge did not happen, the issue is usually in
  workflow or review.
- if a `promote.yml` run on `main` fails in `go test` or another check but a
  later main push promotes successfully, treat the earlier failure as
  transient/superseded unless you can reproduce it on the current head.
- if the run vanished, the issue is usually in teardown or reap.
- if `ward agent list --json` shows `phase: container starting` with
  `status: cleanup-needed` and an empty `started_at`, use the manual stale reservation cleanup path in
  [agent-stale-reservation-cleanup.md](agent-stale-reservation-cleanup.md)
  before host-side debugging.
- if `ward agent list --json` shows `phase: container starting` with an empty
  `started_at`, clear the whole reservation cache directory with
  `ward agent reservations clear` before host-side debugging. The directory is
  cache-only, so wholesale deletion is safe.
- if repo dispatch says the engineer limit is reached but the visible running
  count is still below the ceiling, retry with `--override-reservation` to
  recover stale prelaunch holds. Use `--override-capacity` only for the real
  running-engineer ceiling.
- if Docker says `OOMKilled=true`, treat it as host memory pressure, not a
  normal reap.
- if `sudo` says the no new privileges flag is set, or SSH rejects a
  root-owned include inside the jail, the privileged leg is being asked to
  self-converge inside `ward exec`. Move that leg outside the jail or run it
  from another host.

## Common readings

- if there is no container, start with launch and trust.
- if there is a container but no outcome, start with logs.
- if the death line mentions `OOMKilled=true`, check Docker Desktop or host
  memory pressure first.
- if there is a landed branch but not main, start with workflow.
- if the issue thread has a reservation comment, read that first.
- if the issue thread still carries the reservation marker and the visible run
  is not running, prefer `ward agent reservations clear` over deleting a single
  host file. The cache directory is disposable.

## See also

- [agent-ops.md](agent-ops.md) - logs and reap.
- [agent-lifecycle.md](agent-lifecycle.md) - launch-time checks.
