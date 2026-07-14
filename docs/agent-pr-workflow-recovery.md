---
doc_goal: Describe the closed-unmerged PR recovery path for the native PR merge lane.
---
# ward agent pr merge recovery

When Forgejo closes a PR without merging it, ward treats that as a hard failed
postcondition. The error names the PR number, the head SHA, and the closed
state so the operator can see the exact failure.

The director lane adds one recovery step when the closed PR is still eligible:

1. reopen the PR.
2. re-check the live merge gate.
3. retry the merge once.
4. require `merged: true` again before recording success.

If the reopened PR no longer matches the original head or is no longer green,
ward fails loudly instead of pretending the merge landed.

## See also

- [agent-pr-workflow.md](agent-pr-workflow.md) - the native PR-workflow verbs.
