---
doc_goal: Define the small object, lifecycle, and ownership model behind Ward terminology.
---
# terminology model

One Ward agent lane can be read as:

```text
work item
  -> resolved ref or filed issue
  -> role + harness + workflow selection
  -> dispatch request
  -> broker acceptance or direct host launch
  -> reservation + launch intent
  -> launch checks and pre-flight
  -> ephemeral container / run
  -> logs, transcript, commits, PR or branch evidence
  -> WARD-WORKFLOW terminal or parked outcome
  -> reservation release, cleanup, salvage, or rescue/recovery
```

## Ownership Boundaries

- the tracker thread owns durable work identity, reservation authority, and
  machine-readable workflow comments.
- the dispatch broker owns durable request acceptance and idempotent forwarded
  launch artifacts.
- the host launch path owns pre-flight, credential resolution, Docker plan, and
  container creation.
- the container owns the live workspace, harness process, smoke test,
  implementation attempt, local commits, and teardown.
- the harness owns model or CLI execution. It does not own Ward's workflow
  authority.
- the selected workflow owns what successful delivery means for the run.
- the director owns read-only supervision, backlog heartbeat, redispatch
  sweeps, and merge-ready follow-through.
- `ward exec` owns audited local repo verbs outside the agent container flow.
- release automation owns promotion and published artifacts after work reaches
  `main`.

## Questions Answered

- Work is received by the issue thread or freeform issue-creation path.
- Work beginning creates a dispatch request, reservation, launch intent, and
  eventually one ephemeral container run.
- After a process exits, the issue thread, git history, PR/branch, audit rows,
  logs, dispatch artifact, possible salvage branch, and possible rescue bundle
  can persist.
- Ward supervises dispatch, launch checks, reservations, containers, logs,
  reaping, workflow comments, and PR landing.
- The execution environment owns the workspace clone, mounted context, env, and
  runtime permissions.
- A run reaches terminal or parked workflow state; an issue can close, remain
  open for PR follow-through, or receive later runs.
- `stop` and `logs` affect one run. `reap`, capacity, burndown, and
  dispatch-health operate over wider fleet or backlog state.
- Successful process termination is not successful software delivery. Delivery
  is proven by the workflow's repository or PR evidence.
