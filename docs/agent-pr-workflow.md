---
doc_goal: Describe the native PR-workflow tools (merge, per-PR CI status, Actions run status, rerun) and the embedded role x workflow permission table that gates them.
---
# ward agent pr

`ward agent pr` is native ward code on the compiled Forgejo client, gated by ward's embedded role permission system ([ward#1067](https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/1067)). It does not route through runtime KDL specgen, so a stripped or rolled-back `.ward/` bundle cannot disable it.

## The verbs

- `ward agent pr status <owner/repo#N>` - per-PR combined CI status plus the base branch's required contexts.
- `ward agent pr close <owner/repo#N> --reason TEXT [--supersedes REF]` - close one PR with explicit intent, head-pinned and postcondition checked.
- `ward agent pr reopen <owner/repo#N>` - reopen one closed-unmerged PR with the same head-pinned postcondition check.
- `ward agent pr recover <owner/repo#N>` - diagnose a closed-unmerged PR and report the head SHA, linked issue, and next safe action.
- `ward agent pr merge <owner/repo#N> [--style STYLE]` - merge one PR with permission gate, live required-status gate, head pinning, style resolution, repo-default delete-branch propagation, and merged-state check.
- `ward agent pr runs [owner/repo] [--limit N]` - Actions runs with per-run conclusions.
- `ward agent pr rerun <owner/repo> <run-id>` - rerun one Actions run. The pinned Forgejo API has no rerun operation yet ([agentic-os#434](https://github.com/coilysiren/agentic-os/issues/434)), so this degrades loudly with the manual retrigger fallback.

## The permission table

Merge authority is product data in the embedded role catalog (`merge-authority`), keyed to workflow mode:

- `pull-request` - the director may merge.
- `pull-request-and-merge` - the engineer self-merges, and the director's sweep also lands it.
- `remote-branch-only` / `merge-remote-main` - merge withheld from every role.

Close and reopen use the same merge-authority grant as merge. Status, runs, and recover are read verbs. Rerun needs an `engineering` or `project-management` role. Unknown roles are denied fail-closed.

When a PR repair path is failing, ward prints a concrete bucket first: `ci-parity-gap`, `main-red`, `merge-queue-churn`, or `pr-regression`. That bucket is carried into the PR repair seed and the status readout so the next step names the actual failure mode.

A PR names its own mode: the `ward.workflow:` marker the engineer stamps into a `pull-request-and-merge` PR body. A PR without a marker is the plain `pull-request` lane. PR merges can also take `smart-defaults > pr-merge-style`. `ward agent pr recover` treats `state: closed`, `merged: false` as recovery and points to the next safe action.

If the live PR already has an empty diff against `main`, ward treats that as an already-landed no-op and skips the Forgejo merge endpoint instead of provoking a 500 on a stale PR object.

## Where it runs

- On a read-only director surface, each verb forwards through the host dispatch broker, and host ward re-checks the permission gate before touching the forge.
- Everywhere else (host, engineer container), the verb runs in-process against the Forgejo API.

The `ward agent director merge` composite keeps its stricter thread-driven policy (`WARDED_WORKFLOW`, review, QA verdict); `ward agent pr merge` is the operator-driven single-PR tool under the same status and permission gates.

The recovery and execution-placement details live in
[agent-pr-workflow-recovery.md](agent-pr-workflow-recovery.md).

## See also

- [agent-director.md](agent-director.md) - the read-only director lane.
- [agent-dispatch-broker.md](agent-dispatch-broker.md) - the broker channel.
- [agent-workflow.md](agent-workflow.md) - the workflow-mode model.
- [agent-roles.md](agent-roles.md) - role semantics.
