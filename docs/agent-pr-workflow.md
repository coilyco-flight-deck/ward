---
doc_goal: Describe the native PR-workflow tools and their fixed workflow gate.
---
# ward agent pr

`ward agent pr` is native Ward code on the compiled Forgejo client. It does not
load a role profile or external policy bundle.

## The verbs

- `ward agent pr status <owner/repo#N> [--json]` - per-PR structured CI status with combined status, required status, latest runs, and log hooks.
- `ward agent pr wait <owner/repo#N> [--timeout D] [--interval D] [--head SHA] [--json]` - poll the same status object until the required status turns green.
- `ward agent pr logs <owner/repo#N> [--context NAME]` - follow the status object's executable log hook for the chosen context or the first failing one; unavailable placeholder hooks stay blocked instead of 404ing.
- `ward agent pr close <owner/repo#N> --reason TEXT [--supersedes REF]` - close one PR with explicit intent, head-pinned and postcondition checked.
- `ward agent pr reopen <owner/repo#N>` - reopen one closed-unmerged PR with the same head-pinned postcondition check.
- `ward agent pr recover <owner/repo#N>` - diagnose a closed-unmerged PR and report the head SHA, linked issue, and next safe action.
- `ward agent pr merge <owner/repo#N> [--style STYLE]` - merge one PR with permission gate, live required-status gate, head pinning, style resolution, repo-default delete-branch propagation, and merged-state check.
- `ward agent pr runs [owner/repo] [--limit N]` - Actions runs with per-run conclusions.
- `ward agent pr rerun <owner/repo> <run-id>` - rerun one Actions run. The pinned Forgejo API has no rerun operation yet, so this degrades loudly with the manual retrigger fallback.

## The workflow gate

Status, logs, runs, recovery, and rerun are fixed operations. Close, reopen,
and merge require the PR's `pull-request-and-merge` workflow marker. The acting
role string is opaque metadata and cannot grant or attenuate an operation.

A PR names its mode with the `ward.workflow:` marker stamped into a
`pull-request-and-merge` PR body. `ward agent pr recover` treats
`state: closed`, `merged: false` as recovery and points to the next safe action.

See [agent-human-feedback.md](agent-human-feedback.md).

## Where it runs

- On a read-only director surface, each verb forwards through the supervised
  dispatch broker, and broker Ward re-checks the workflow gate before touching
  the forge.
- Everywhere else (host, engineer container), the verb runs in-process against the Forgejo API.

The status, wait, and log follow-up object is documented in
[agent-pr-status-object.md](agent-pr-status-object.md).

The `ward agent director merge` composite keeps its stricter thread-driven
policy (`WARD-WORKFLOW:`, review, QA verdict). `ward agent pr merge` is the
operator-driven single-PR tool under the same status and workflow gates.

The recovery and execution-placement details live in [agent-pr-workflow-recovery.md](agent-pr-workflow-recovery.md).

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-dispatch-broker.md](agent-dispatch-broker.md) - the broker channel.
- [agent-workflow.md](agent-workflow.md) - the workflow-mode model.
- [agent-roles.md](agent-roles.md) - role semantics.
