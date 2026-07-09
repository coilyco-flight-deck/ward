---
doc_goal: Explain the director's narrow PR-merge authority boundary - which ward-owned PRs it may merge, which ones it must leave alone, and how that differs from the engineer's own pull-request workflow.
---
# Director PR merge boundary

`ward agent director` can do one narrow write action beyond dispatch: merge a
ward-owned pull request when the run was launched in
`pull-request-and-merge`.

Ordinary open PRs still show up in the director ledger as `pull-request` lane
items. The heartbeat tracks them and surfaces them with `PR #N` identity, but
only the merge sweep below may land them.

## What qualifies

The merge sweep only acts when all of these are true:

- the PR is open, not draft, and not on a `ward-salvage/` branch;
- the PR body carries the `ward.workflow: pull-request-and-merge` marker;
- the PR body names the carried issue with `closes #N` / `fixes #N` / `resolves #N`;
- the linked issue is already `done`;
- the latest issue thread has a passing `WARD-QA` verdict for the PR's current head SHA.

The review-summary prose is still recorded, but it is informational only. Merge
eligibility follows the commit-bound QA verdict, not the freeform summary.

If any check fails, the director reports the reason and leaves the PR alone.

## What it does not do

- It does not merge arbitrary human PRs by default.
- It does not merge plain `pull-request` runs unless a later policy explicitly
  says to.
- It does not treat salvage PRs as eligible work PRs.
- It does not accept stale QA comments for an older PR head SHA.

## How this differs from engineer workflow

The engineer's `pull-request` mode keeps watching CI after the PR opens and
only reports done once the checks are green or the failure is genuinely blocked.
`pull-request-and-merge` adds the director marker so the heartbeat can finish
the merge later when the policy boundary is satisfied, and the run is not done
until the merge lands.

## See also

- [agent-workflow.md](agent-workflow.md) - the run landing policy that sets the marker.
- [agent-director.md](agent-director.md) - the heartbeat that runs the merge sweep.
