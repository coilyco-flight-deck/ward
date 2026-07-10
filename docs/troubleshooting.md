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

## What to check

- `~/.ward/agent-logs/<container>/`.
- the reservation comment on the issue.
- the preflight or trust gate that blocked the launch.

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
- if the run vanished, the issue is usually in teardown or reap.
- if Docker says `OOMKilled=true`, treat it as host memory pressure, not a
  normal reap.

## Common readings

- if there is no container, start with launch and trust.
- if there is a container but no outcome, start with logs.
- if the death line mentions `OOMKilled=true`, check Docker Desktop or host
  memory pressure first.
- if there is a landed branch but not main, start with workflow.
- if the issue thread has a reservation comment, read that first.

## See also

- [agent-ops.md](agent-ops.md) - logs and reap.
- [agent-lifecycle.md](agent-lifecycle.md) - launch-time checks.
