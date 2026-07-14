---
doc_goal: Capture the landing-policy surface in one place so the run modes and review gate are readable without the old per-issue split pages.
---
# ward agent workflow

`--workflow` chooses how a run lands.

- `merge-remote-main` - merge to `main` and close.
- `pull-request` - open a PR and watch it to green.
- `pull-request-and-merge` - open a PR, wait for merge readiness, then let the director merge sweep finish the landing. The machine-readable `ward.workflow` marker uses the canonical `pull-request-and-merge` spelling.
- `remote-branch-only` - publish a branch only.

Legacy aliases `direct-main`, `direct-to-main`, `pull-requests`,
`pull-requests-and-merge`, and `patch-only` are still accepted with warnings.
`pr` is not accepted.

The machine-readable issue-comment prefix for ward-authored workflow updates is
`WARDED_WORKFLOW`. Legacy `WARD-*` issue-comment headers still parse on old
threads, but new PR handoffs emit `WARDED_WORKFLOW: <fully-qualified pull request link>` instead of the old submitted marker. Review-gated `pull-request-and-merge` handoffs carry `director merge authorization: reviewed-and-ready` in the collapsed details so the director can still treat the run as merge-ready.

## Review

The review gate runs before a PR is opened or merged when enabled. It is a
fail-closed gate, not an advisory comment.

### Review expectations

- a reviewer error blocks the landing.
- an empty vote blocks the landing.
- a timeout blocks the landing.
- a green review is still subject to CI.

## What the PR modes guarantee

- They keep the run visible after the branch opens.
- They preserve the actionable failure comment on the issue thread.
- They keep merge authority separate from launch authority.

## Mode details

### merge-remote-main

The run lands directly on `main`. This is the shortest path and the least
visible path.

### pull-request

The run opens a branch and a PR, then keeps watching until the PR is green.
Failure comments stay on the issue, and the PR copy mirrors the actionable
message when one already exists.
When the PR is opened, the issue thread's workflow comment starts with the
canonical pull request URL.

### pull-request-and-merge

This is the director-merge lane. The worker gets the PR ready, and the
director merge sweep finishes the landing once the checks and review are
green. The issue comment starts with the canonical PR URL and records the
reviewed-and-ready authorization in its details block.
The final machine-readable workflow marker uses `pull-request-and-merge`.

### remote-branch-only

The run produces a branch and stops there.

## Why the workflow matters

The workflow decides who is allowed to close the loop.

- launch and implementation happen in the worker.
- review and merge readiness happen in the workflow gate.
- the director records the final merge state when that lane is used.

## See also

- [agent-director.md](agent-director.md) - the merge-ready director lane.
- [dispatch-review.md](dispatch-review.md) - the review gate details.
- [agent-lifecycle.md](agent-lifecycle.md) - launch-time checks.
