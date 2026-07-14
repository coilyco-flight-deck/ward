---
doc_goal: Describe the closed-unmerged PR recovery path for the native PR merge lane.
---
# ward agent pr merge recovery

When Forgejo closes a PR without merging it, ward treats that as a hard failed
postcondition. The error names the PR number, the head SHA, and the closed
state so the operator can see the exact failure.

PR `ward#1002` is the historical example here: Forgejo reported the required
`test / test (pull_request)` context as red, the PR closed with `merged=false`,
and ward kept that as a fail-closed outcome instead of pretending it landed.

The director lane adds one recovery step when the closed PR is still eligible:

1. reopen the PR.
2. re-check the live merge gate.
3. retry the merge once.
4. require `merged: true` again before recording success.

If the reopened PR no longer matches the original head or is no longer green,
ward fails loudly instead of pretending the merge landed.

## Where it runs

- On a **read-only director surface**, each verb forwards through the host
  dispatch broker, and host ward re-checks the permission gate before touching
  the forge.
- Everywhere else, the verb runs in-process against the Forgejo API with the
  session's own credential.

The `ward agent director merge` composite keeps its stricter thread-driven
policy (`WARDED_WORKFLOW`, review, QA verdict). `ward agent pr merge` is the
operator-driven single-PR tool under the same gates.

## See also

- [agent-pr-workflow.md](agent-pr-workflow.md) - the native PR-workflow verbs.
