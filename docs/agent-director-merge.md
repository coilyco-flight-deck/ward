---
doc_goal: Explain the director's narrow PR-merge lane, including the policy boundary, the eligible PR shape, and the reasons it stays distinct from the normal human-gated pull-requests flow.
---
# ward agent director merge

`ward agent director merge` scans the open pull requests in scope and merges only
the ones whose linked issue thread authorizes it.

## Policy boundary

The merge lane is narrow by design. It accepts only ward-owned work whose carried
issue thread says:

- `workflow: pull-requests-and-merge`
- `WARD-OUTCOME: merge-ready`
- review summary starts with `passed:`
- the PR title is not salvage or WIP noise

That keeps `pull-requests` human-gated. The director does not gain a
general PR-review or blanket repo-write surface here.
After the merge lands, the director records the final `WARD-OUTCOME: done` on
the issue.

## What it does

The command looks up the open PRs in scope, joins each PR back to its linked issue
via a same-repo closing reference, reads the latest `WARD-OUTCOME` comment, and
merges the PR only when the policy above holds.

## See also

- [agent-director.md](agent-director.md) - the supervisor loop that owns the lane.
- [agent-workflow.md](agent-workflow.md) - the workflow mode that marks a run merge-eligible.
