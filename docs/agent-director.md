---
doc_goal: Give the director surface one durable description so the read-only lane and its merge-ready follow-through do not live in scattered issue pages.
---
# ward agent director

The director surface is the read-only control plane for runs.

- It can inspect the fleet, read logs, and stop a run.
- It can keep a backlog moving without writing implementation code.
- It is the surface that hosts the merge-ready workflow for PR landings.

## Why it exists

- Interactive work should not share the detached engineer path.
- Supervisory work needs a read-only session with brokered access.
- Merge follow-up needs a lane that can verify checks before merge.

## Typical uses

- check whether an engineer is still alive.
- read the last logs before deciding whether to re-dispatch.
- inspect the queue/status view for stale reservations, redispatch candidates, submitted PRs, and stale-open done issues.
- stop a run that is definitely on the wrong ref.
- sweep the merge-ready branch once CI is green.

The director surface is intentionally narrower than the engineer path. It is
for supervision and landing, not for implementation.

## What it is not

- it is not a shell into the target repo.
- it is not a general-purpose container admin surface.
- it is not a replacement for the issue thread.

## See also

- [agent-ops.md](agent-ops.md) - list, logs, stop, reap.
- [agent-workflow.md](agent-workflow.md) - PR and merge policy.
- [agent-roles.md](agent-roles.md) - role semantics.
