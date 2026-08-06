---
doc_goal: Define Ward's fixed landing workflows, canonical machine state, review behavior, and successful-delivery evidence.
---
# Agent workflows

`--workflow` selects how a successful run delivers work.

* `merge-remote-main` - land on remote `main` and close the issue.
* `pull-request` - publish a branch and PR, then observe checks under the sealed worker boundary.
* `pull-request-and-merge` - publish a reviewed, merge-ready PR for the director merge lane.
* `remote-branch-only` - publish a remote branch and stop.

Legacy aliases remain compatibility input with warnings. `pr` is not an alias.
New machine-readable workflow comments start with `WARD-WORKFLOW:`. Older
typed headers remain parser input only.

## Review gate

When enabled, review runs before opening or merging a PR. It examines the
candidate diff and current filesystem state under the configured review class.
A reviewer error, empty vote, timeout, or rejection blocks landing. Approval
does not replace CI or the exact-revision QA gate.

## Evidence

* Direct landing requires the candidate on current remote `main`.
* Pull-request landing requires the remote branch, canonical PR URL, and
  submitted workflow state.
* Director landing requires current CI, review authorization, any required QA
  verdict for the current head, and final `merged: true`.
* Branch-only delivery requires the named remote branch.

Harness exit, a local commit, or a stale no-diff salvage branch is not delivery.
Teardown rechecks current landing evidence before reporting failure or
reopening an issue.

## See also

* [agent-pr-workflow.md](agent-pr-workflow.md) - PR status and mutation verbs.
* [agent-roles.md](agent-roles.md) - sealed worker behavior.
* [container-lifecycle.md](container-lifecycle.md) - teardown proof and rescue.
