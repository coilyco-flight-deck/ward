---
doc_goal: Define Ward's complete native pull-request status, wait, logs, recovery, actor gate, and mutation contract.
---
# Pull-request operations

`ward agent pr` uses Ward's compiled forge client. Read-only director calls
forward through the supervised broker. Host and worker calls run in process
with their own allowed credential surface.

## Verbs

* `status <owner/repo#N> [--json]` returns PR, head, required and combined CI,
  contexts, current-head runs, log hooks, repair class, and next action.
* `wait <owner/repo#N> [--timeout D] [--interval D] [--head SHA] [--json]`
  exits 0 on green, 1 on terminal red or head mismatch, 124 on timeout, and 2
  on usage or auth failure.
* `logs <owner/repo#N> [--context NAME]` follows the selected status object's
  executable log hook. Unavailable placeholder hooks remain blocked.
* `recover <owner/repo#N>` diagnoses a closed-unmerged PR and names its head,
  linked issue, and next safe action.
* `close ... --reason TEXT [--supersedes REF]` and `reopen ...` are head-pinned
  and require postcondition checks.
* `merge ... [--style STYLE]` enforces workflow, permission, live required
  status, head pin, merge style, branch deletion, and `merged: true`.
* `runs [owner/repo]` reads current runs. `rerun <owner/repo> <run-id>` fails
  loudly with a manual fallback where the forge API lacks rerun support.

## Workflow and revision gates

Status, logs, runs, recovery, and rerun are fixed reads or actions. Close,
reopen, and merge require `pull-request-and-merge` machine state. The director
merge composite additionally requires thread workflow state, reviewed-and-ready
authorization, current CI, and an exact-revision QA verdict when configured.

A closed-unmerged PR is failure, not landing. Recovery may reopen it, re-read
the live gates, retry once, and again require `merged: true`. Head drift blocks
the retry.

## Human feedback and actor authority

Machine state is admitted by authenticated author and fixed record kind, not
marker-shaped prose. Deployment configures exact trusted collaborators and one
automation actor. Their identities must be present and disjoint. Only the
automation actor can mint Ward machine records. A trusted collaborator's prose
is human input even when it resembles a marker.

External issue or PR text enters model prompts only after a trusted
collaborator approves an exact snapshot and the director broker seals it with
`ward agent issue approve`. Any edit, missing selected comment, actor-policy
change, or later unacknowledged input invalidates the snapshot.

## See also

* [agent-workflow.md](agent-workflow.md) - landing modes and review.
* [agent-dispatch-broker.md](agent-dispatch-broker.md) - credential and actor checks.
* [agent-roles.md](agent-roles.md) - QA revision contract.
